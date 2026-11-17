package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/engine"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func (a *app) syncCommand() *cobra.Command {
	var dry bool
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Reconcile exclusions with the config (-n: dry run with sizes)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runSync(dry)
		},
	}
	cmd.Flags().BoolVarP(&dry, "dry-run", "n", false, "preview: change nothing, show sizes")
	return cmd
}

type jsonAction struct {
	Op     string `json:"op"`
	Path   string `json:"path"`
	Rule   string `json:"rule,omitempty"`
	Reason string `json:"reason,omitempty"`
	Bytes  *int64 `json:"bytes,omitempty"`
}

type jsonProblem struct {
	Path   string `json:"path"`
	Error  string `json:"error"`
	Denied bool   `json:"denied"`
}

type syncSummary struct {
	Excluded  int `json:"excluded"`
	Included  int `json:"included"`
	Unchanged int `json:"unchanged"`
	UserOwned int `json:"user_owned"`
}

type syncJSON struct {
	DryRun   bool          `json:"dry_run"`
	Config   string        `json:"config"`
	Actions  []jsonAction  `json:"actions"`
	Summary  syncSummary   `json:"summary"`
	Problems []jsonProblem `json:"problems"`
	Warnings []string      `json:"warnings"`
}

// syncOutcome is a finished sync (or dry run), ready to print.
type syncOutcome struct {
	dry      bool
	actions  []engine.Action
	problems []engine.Problem
	summary  syncSummary
	code     int
}

// runSync is `tmskip sync [-n]`; init reuses it.
func (a *app) runSync(dry bool) error {
	if !dry {
		unlock, err := a.lock()
		if err != nil {
			return err
		}
		defer unlock()
	}
	r, _, err := a.loadConfig()
	if err != nil {
		return a.afterFailedRun(err, false)
	}
	res, err := a.syncWith(r, dry)
	if err != nil {
		return a.afterFailedRun(err, r.Notify)
	}
	if a.d.Env.Scheduled() && !dry {
		unreadable := 0
		for _, p := range res.problems {
			if !p.Denied {
				unreadable++
			}
		}
		a.afterScheduledRun(problemKeys(res.problems, nil, nil), unreadable, r.Notify)
	}
	a.printSync(r, res)
	if res.code != ExitOK {
		return exitWith(res.code, nil)
	}
	return nil
}

// syncWith scans, reconciles and (unless dry) applies, saving state and
// the log.
func (a *app) syncWith(r *config.Resolved, dry bool) (*syncOutcome, error) {
	fs := a.d.FS
	paths := a.statePaths()
	st, warn, err := state.Load(fs, paths)
	if err != nil {
		return nil, exitWith(ExitError, err)
	}
	if warn != "" {
		a.out.Warn("%s", warn)
	}
	ex := exclusion.New(fs)
	prog := a.progress()
	scan := engine.ScanWith(r, fs, ex, prog.engine())
	prog.Clear()
	for _, b := range scan.Blocked {
		a.out.Verbose("  %s  (%s match, but it %s)", b.Path, b.Match.Rule, b.Guard)
	}
	plan := engine.Reconcile(r, fs, ex, scan, st.Owned)
	res := &syncOutcome{dry: dry, problems: plan.Problems}

	if dry {
		res.actions = plan.Actions
		var measure []string
		for _, ac := range plan.Actions {
			if ac.Op == engine.OpExclude || ac.Op == engine.OpInclude {
				measure = append(measure, ac.Path)
			}
		}
		sizes := engine.SizesWith(fs, measure, runtime.NumCPU(), prog.engine())
		prog.Clear()
		for i := range res.actions {
			if n, ok := sizes[res.actions[i].Path]; ok {
				res.actions[i].Bytes = n
			}
		}
	} else {
		applied, hints, applyProblems := engine.Apply(r, fs, ex, plan)
		done := map[string]bool{}
		var excludedNow []string
		for _, ac := range applied {
			done[ac.Path] = true
			if ac.Op == engine.OpExclude {
				excludedNow = append(excludedNow, ac.Path)
			}
		}
		sizes := engine.SizesWith(fs, excludedNow, runtime.NumCPU(), prog.engine())
		prog.Clear()
		for _, ac := range plan.Actions {
			if (ac.Op == engine.OpExclude || ac.Op == engine.OpInclude) && !done[ac.Path] {
				continue // failed: reported as a problem
			}
			if n, ok := sizes[ac.Path]; ok && ac.Op == engine.OpExclude {
				ac.Bytes = n
			}
			res.actions = append(res.actions, ac)
		}
		for i, h := range hints {
			if n, ok := sizes[h.Path]; ok {
				n := n
				hints[i].Bytes = &n
			}
		}
		st.Owned = hints
		res.problems = append(res.problems, applyProblems...)
		res.code = exitCodeFor(plan.Problems, applyProblems)
		res.summary = summarize(res.actions)
		st.LastRun = &state.Run{
			At: a.d.Env.Now(), Scheduled: a.d.Env.Scheduled(), ExitCode: res.code,
			Excluded: res.summary.Excluded, Included: res.summary.Included, Unchanged: res.summary.Unchanged,
			Problems: runProblems(res.problems),
		}
		if err := state.Save(fs, paths, st); err != nil {
			return nil, exitWith(ExitError, fmt.Errorf("saving state: %w", err))
		}
		a.logSync(res)
	}
	res.summary = summarize(res.actions)
	if dry {
		res.code = exitCodeFor(plan.Problems, nil)
	}
	return res, nil
}

// exitCodeFor: an apply failure that isn't a read problem is an error (1);
// otherwise any problem means "completed with unreadable folders" (3).
func exitCodeFor(readProblems, applyProblems []engine.Problem) int {
	for _, p := range applyProblems {
		if !fsys.IsUnreadable(p.Err) {
			return ExitError
		}
	}
	if len(readProblems)+len(applyProblems) > 0 {
		return ExitUnreadable
	}
	return ExitOK
}

func summarize(actions []engine.Action) syncSummary {
	var s syncSummary
	for _, ac := range actions {
		switch ac.Op {
		case engine.OpExclude:
			s.Excluded++
		case engine.OpInclude:
			s.Included++
		case engine.OpKeep:
			s.Unchanged++
		case engine.OpUserOwned:
			s.UserOwned++
		}
	}
	return s
}

func runProblems(ps []engine.Problem) []state.RunProblem {
	out := []state.RunProblem{}
	for _, p := range ps {
		out = append(out, state.RunProblem{Path: p.Path, Error: errText(p.Err), Denied: p.Denied})
	}
	return out
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return fsys.ErrText(err)
}

func (a *app) logSync(res *syncOutcome) {
	kind := "manual"
	if a.d.Env.Scheduled() {
		kind = "scheduled"
	}
	lines := []string{fmt.Sprintf("sync %s exit=%d +%d -%d =%d problems=%d",
		kind, res.code, res.summary.Excluded, res.summary.Included, res.summary.Unchanged, len(res.problems))}
	for _, ac := range res.actions {
		switch ac.Op {
		case engine.OpExclude:
			lines = append(lines, fmt.Sprintf("+ %s (%s)", ac.Path, ac.Rule))
		case engine.OpInclude:
			lines = append(lines, fmt.Sprintf("- %s (%s)", ac.Path, ac.Note))
		}
	}
	for _, p := range res.problems {
		lines = append(lines, fmt.Sprintf("! %s: %s", p.Path, errText(p.Err)))
	}
	if err := state.AppendLog(a.d.FS, a.statePaths(), a.d.Env.Now(), lines...); err != nil {
		a.out.Warn("writing the log: %v", err)
	}
}

// actionLine renders one action for the terminal.
func (a *app) actionLine(ac engine.Action) string {
	detail := ac.Rule
	if ac.Op == engine.OpInclude || ac.Op == engine.OpUserOwned || ac.Op == engine.OpUntouched {
		detail = ac.Note
	}
	if ac.Bytes > 0 && (ac.Op == engine.OpExclude || ac.Op == engine.OpInclude) {
		detail += ", " + humanBytes(ac.Bytes)
	}
	sym := ac.Op.Symbol()
	switch ac.Op {
	case engine.OpExclude:
		sym = a.out.green(sym)
	case engine.OpInclude:
		sym = a.out.yellow(sym)
	case engine.OpUntouched:
		sym = a.out.red(sym)
	}
	return fmt.Sprintf("%s %s  (%s)", sym, ac.Path, detail)
}

func (a *app) printSync(r *config.Resolved, res *syncOutcome) {
	for _, p := range res.problems {
		a.out.Problem(p.Path, errText(p.Err))
	}
	if len(r.Unreadable) > 0 {
		var ps []string
		for _, u := range r.Unreadable {
			ps = append(ps, u.Path)
		}
		a.out.Warn("nothing was changed: %s can't be resolved, so the config can't be applied safely — tmskip changes nothing until it can be read (Full Disk Access?)", strings.Join(ps, ", "))
	}
	if a.g.json {
		doc := syncJSON{DryRun: res.dry, Config: r.Path, Actions: []jsonAction{}, Summary: res.summary,
			Problems: jsonProblems(res.problems), Warnings: nonNil(a.out.warnings)}
		for _, ac := range res.actions {
			ja := jsonAction{Op: ac.Op.String(), Path: ac.Path}
			if ac.Op == engine.OpExclude || ac.Op == engine.OpKeep || ac.Op == engine.OpUserOwned {
				ja.Rule = ac.Rule
			}
			if ac.Op == engine.OpInclude || ac.Op == engine.OpUntouched {
				ja.Reason = ac.Note
			}
			if ac.Bytes > 0 {
				b := ac.Bytes
				ja.Bytes = &b
			}
			doc.Actions = append(doc.Actions, ja)
		}
		a.out.JSON(doc)
		return
	}
	for _, ac := range res.actions {
		switch ac.Op {
		case engine.OpExclude, engine.OpInclude:
			a.out.Line("%s", a.actionLine(ac))
		case engine.OpUntouched:
			if !strings.HasPrefix(ac.Note, "unreadable") { // unreadable ones are listed as problems
				a.out.Line("%s", a.actionLine(ac))
			}
		default:
			a.out.Verbose("%s", a.actionLine(ac))
		}
	}
	s := res.summary
	line := fmt.Sprintf("+%d −%d =%d", s.Excluded, s.Included, s.Unchanged)
	if s.UserOwned > 0 {
		line += fmt.Sprintf("  (%d excluded by you, left alone)", s.UserOwned)
	}
	if res.dry {
		line += "  (dry run — nothing changed)"
		a.out.Hint("apply: tmskip sync")
	} else {
		a.out.Hint("see what's managed: tmskip list")
	}
	a.out.Line("%s", line)
}

func jsonProblems(ps []engine.Problem) []jsonProblem {
	out := []jsonProblem{}
	for _, p := range ps {
		out = append(out, jsonProblem{Path: p.Path, Error: errText(p.Err), Denied: p.Denied})
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

package engine

import (
	"sort"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// Op is what a plan does to one path.
type Op int

const (
	OpExclude   Op = iota + 1 // + mark, then exclude
	OpInclude                 // - re-include, then unmark
	OpKeep                    // = already owned and excluded
	OpUserOwned               // · excluded by the user (no marker): never touched
	OpUntouched               // ! couldn't be decided (unreadable, unmounted): left as is
)

func (o Op) String() string {
	switch o {
	case OpExclude:
		return "exclude"
	case OpInclude:
		return "include"
	case OpKeep:
		return "keep"
	case OpUserOwned:
		return "user_owned"
	case OpUntouched:
		return "untouched"
	}
	return "unknown"
}

// Symbol is the op's output prefix.
func (o Op) Symbol() string {
	switch o {
	case OpExclude:
		return "+"
	case OpInclude:
		return "-"
	case OpKeep:
		return "="
	case OpUserOwned:
		return "·"
	case OpUntouched:
		return "!"
	}
	return "?"
}

// Action is one step of a plan.
type Action struct {
	Op    Op
	Path  string
	Rule  string // for exclude/keep
	Note  string // why (include), or what's wrong (untouched)
	Bytes int64  // size, when known (0 = unknown)
}

// Plan is what a sync (or uninstall) will do.
type Plan struct {
	Actions  []Action
	Hints    []state.Owned // the hint list assuming the plan applies cleanly
	Problems []Problem     // scan problems + paths whose state couldn't be read
}

// Reconcile compares what should be excluded (scan) with what tmskip owns
// (markers on candidates from the scan and the hint list).
func Reconcile(r *config.Resolved, fs fsys.FS, ex exclusion.Store, scan *ScanResult, hints []state.Owned) *Plan {
	plan := &Plan{Problems: append([]Problem(nil), scan.Problems...)}
	if len(r.Unreadable) > 0 {
		// A root, keep or fixed path couldn't be resolved: the config can't be
		// applied safely (a kept folder could be excluded, an owned one
		// re-included). Change nothing, keep every hint, report the problems.
		plan.Hints = append([]state.Owned(nil), hints...)
		return plan
	}
	hints = CanonicalHints(fs, hints) // all comparisons use canonical paths
	prevBytes := map[string]*int64{}
	isHint := map[string]bool{}
	candidates := map[string]bool{}
	for p := range scan.Desired {
		candidates[p] = true
	}
	for _, p := range scan.MarkedSeen {
		candidates[p] = true
	}
	for _, h := range hints {
		candidates[h.Path] = true
		isHint[h.Path] = true
		prevBytes[h.Path] = h.Bytes
	}
	paths := make([]string, 0, len(candidates))
	for p := range candidates {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	keepHint := func(p string) { plan.Hints = append(plan.Hints, state.Owned{Path: p, Bytes: prevBytes[p]}) }
	act := func(op Op, p, rule, note string) {
		a := Action{Op: op, Path: p, Rule: rule, Note: note}
		if b := prevBytes[p]; b != nil && (op == OpKeep || op == OpUntouched) {
			a.Bytes = *b
		}
		plan.Actions = append(plan.Actions, a)
	}
	untouched := func(p string, err error) {
		if fsys.IsNotExist(err) {
			return // vanished since the walk (e.g. a build deleted it): nothing to do
		}
		plan.Problems = append(plan.Problems, newProblem(p, err))
		act(OpUntouched, p, "", "unreadable: "+errText(err))
		if isHint[p] {
			keepHint(p)
		}
	}

	for _, p := range paths {
		if d, ok := scan.Desired[p]; ok {
			marked, err := ex.IsMarked(p)
			if err != nil {
				untouched(p, err)
				continue
			}
			excluded, err := ex.IsExcluded(p)
			if err != nil {
				untouched(p, err)
				continue
			}
			switch {
			case !marked && excluded:
				act(OpUserOwned, p, d.Match.Rule, "excluded by you, not by tmskip")
			case marked && excluded:
				act(OpKeep, p, d.Match.Rule, "")
				keepHint(p)
			default: // new, or marked but not excluded (repair)
				act(OpExclude, p, d.Match.Rule, "")
				keepHint(p)
			}
			continue
		}

		d := Evaluate(r, fs, p, false)
		switch d.Reason {
		case ReasonMissing:
			continue // gone from a mounted volume: drop the hint
		case ReasonUnmounted:
			act(OpUntouched, p, "", "volume not mounted")
			keepHint(p)
			continue
		case ReasonUnreadable:
			untouched(p, d.Err)
			continue
		}
		marked, err := ex.IsMarked(p)
		if err != nil {
			untouched(p, err)
			continue
		}
		if !marked {
			continue // not ours (any more): drop the hint
		}
		excluded, err := ex.IsExcluded(p)
		if err != nil {
			untouched(p, err)
			continue
		}
		if d.Exclude { // still matches, e.g. nested inside a matched folder
			if excluded {
				act(OpKeep, p, d.Match.Rule, "")
			} else {
				act(OpExclude, p, d.Match.Rule, "")
			}
			keepHint(p)
			continue
		}
		act(OpInclude, p, d.Match.Rule, includeNote(d))
	}
	return plan
}

// includeNote says why a folder is re-included.
func includeNote(d Decision) string {
	switch d.Reason {
	case ReasonNotMatched:
		return "no rule matches"
	case ReasonSkipped:
		return "inside a skipped folder"
	case ReasonOutsideRoots:
		return "outside roots and fixed paths"
	case ReasonBlocked:
		return d.Guard
	}
	return d.Reason.String()
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return fsys.ErrText(err)
}

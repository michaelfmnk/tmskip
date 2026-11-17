package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/engine"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func (a *app) uninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Schedule off, re-include everything tmskip excluded, drop state",
		Long: "Turns the schedule off, re-includes every folder tmskip excluded (walking roots, fixed paths\n" +
			"and the folders recorded in state), removes the markers and drops tmskip's state.\n" +
			"The binary, the config and the logs are left.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return a.runUninstall() },
	}
}

func (a *app) runUninstall() error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()

	var scheduleErr error
	if a.d.Launchd != nil {
		if scheduleErr = a.scheduleOff(); scheduleErr != nil {
			a.out.Errorf("turning the schedule off: %v", scheduleErr)
		}
	}

	r, _, err := a.loadConfig()
	if err != nil {
		a.out.Warn("config unusable — sweeping only folders recorded in state (%s)", firstLine(err.Error()))
		r = nil
	}
	fs := a.d.FS
	paths := a.statePaths()
	st, warn, err := state.Load(fs, paths)
	if err != nil {
		return exitWith(ExitError, err)
	}
	if warn != "" {
		a.out.Warn("%s", warn)
	}
	ex := exclusion.New(fs)
	plan := engine.Sweep(r, fs, ex, st.Owned)
	var cr *config.Resolved // nil: no guardrail re-check (only exclusions need it)
	applied, hints, applyProblems := engine.Apply(cr, fs, ex, plan)

	for _, ac := range applied {
		a.out.Line("%s", a.actionLine(engine.Action{Op: ac.Op, Path: ac.Path, Note: "re-included"}))
	}
	for _, ac := range plan.Actions {
		if ac.Op == engine.OpUntouched && ac.Note == "volume not mounted" {
			a.out.Line("%s", a.actionLine(ac))
		}
	}
	problems := append(plan.Problems, applyProblems...)
	for _, p := range problems {
		a.out.Problem(p.Path, errText(p.Err))
	}
	code := exitCodeFor(plan.Problems, applyProblems)
	if code == ExitOK && len(hints) > 0 {
		code = ExitUnreadable // e.g. folders on an unmounted volume: rerun when it's back
	}
	if scheduleErr != nil {
		// The agent may still be loaded and re-exclude: not uninstalled yet.
		code = ExitError
	}
	lines := []string{fmt.Sprintf("uninstall exit=%d -%d problems=%d", code, len(applied), len(problems))}
	for _, ac := range applied {
		lines = append(lines, "- "+ac.Path+" (uninstall)")
	}
	a.log(lines...)

	if code == ExitOK {
		unlock()
		locked = false
		if err := fs.RemoveAll(paths.Dir); err != nil {
			return exitWith(ExitError, fmt.Errorf("removing %s: %w", paths.Dir, err))
		}
		a.out.Line("tmskip is uninstalled: %s re-included, state removed. The config and logs are left.", plural(len(applied), "folder"))
	} else {
		st.Owned = hints
		if err := state.Save(fs, paths, st); err != nil {
			return exitWith(ExitError, err)
		}
		a.out.Line("%s re-included; %s left — fix the problems above and run \"tmskip uninstall\" again.", plural(len(applied), "folder"), plural(len(hints), "folder"))
		if scheduleErr != nil {
			a.out.Errorf("the schedule couldn't be turned off, so tmskip is not uninstalled yet — run \"tmskip uninstall\" again (after logging out and in, if launchd keeps refusing)")
		}
	}
	if code == ExitOK {
		a.out.Hint("remove the binary: brew uninstall tmskip")
	} else {
		a.out.Hint("try again: tmskip uninstall")
	}
	if code != ExitOK {
		return exitWith(code, nil)
	}
	return nil
}

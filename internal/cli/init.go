package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/launchd"
)

func (a *app) initCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Guided setup: config, preview, apply, Full Disk Access, schedule",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runInit() },
	}
}

// say prints init's own text (shown even without hints; not under -q).
func (a *app) say(format string, args ...any) { a.out.Line(format, args...) }

func (a *app) runInit() error {
	if !a.d.Env.StdinTTY || !a.d.Env.StdoutTTY {
		return errorf(ExitError, `init is interactive; use "tmskip config reset" instead`)
	}
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()

	// 1. Config.
	path := a.configPath()
	if _, err := a.d.FS.Lstat(path); err == nil {
		a.say("Config already exists at %s — keeping it.", path)
	} else {
		var roots []string
		found := detectRoots(a.d.Env, a.d.FS)
		if len(found) == 0 {
			a.say("No usual code folder found (~/Developer, ~/Projects, ~/src, …); add yours to roots in the config.")
		}
		for _, root := range found {
			ok, err := a.ask("Scan "+root+"?", true)
			if err != nil {
				return exitWith(ExitError, err)
			}
			if ok {
				roots = append(roots, root)
			}
		}
		if err := config.WriteThrough(a.d.FS, path, []byte(config.Template(roots))); err != nil {
			return exitWith(ExitError, fmt.Errorf("writing %s: %w", path, err))
		}
		a.say("Wrote %s — every rule tmskip applies is in it.", path)
		a.log("init wrote " + path)
	}

	r, _, err := a.loadConfig()
	if err != nil {
		return err
	}

	// 2. Preview, 3. apply.
	preview, err := a.syncWith(r, true) // scans with a progress line
	if err != nil {
		return err
	}
	a.say("")
	a.say("Preview (nothing is changed yet):")
	a.printSync(r, preview)
	code := ExitOK
	if ok, err := a.ask("Apply now?", true); err != nil {
		return exitWith(ExitError, err)
	} else if ok {
		res, err := a.syncWith(r, false)
		if err != nil {
			return err
		}
		a.printSync(r, res)
		code = res.code
	}

	// 4. Full Disk Access, only when a root or fixed path needs it.
	if needed := needsFDA(r); len(needed) > 0 {
		bin, _, _ := launchd.BinaryPath(a.d.Env)
		a.say("")
		a.say("These are in folders macOS protects: %s", strings.Join(needed, ", "))
		a.say("The scheduled run can only read them with Full Disk Access. In System Settings →")
		a.say("Privacy & Security → Full Disk Access, click +, press ⌘⇧G and add %s", bin)
		ok, err := a.ask("Open Full Disk Access settings now?", true)
		if err != nil {
			return exitWith(ExitError, err)
		}
		if ok && a.d.Runner != nil {
			if _, stderr, code, err := a.d.Runner.Run("/usr/bin/open", fdaSettingsURL); err != nil || code != 0 {
				a.out.Warn("couldn't open System Settings: %v %s", err, strings.TrimSpace(string(stderr)))
			}
		}
	}

	// 5. Schedule.
	if r.Interval > 0 && a.d.Launchd != nil {
		ok, err := a.ask(fmt.Sprintf("Turn on the schedule (every %s)?", launchd.Interval(r.Interval)), true)
		if err != nil {
			return exitWith(ExitError, err)
		}
		if ok {
			if err := a.scheduleOn(r); err != nil {
				return err
			}
		}
	}
	a.say("")
	a.say("Done. Change the rules in %s; preview with \"tmskip sync -n\".", path)
	a.out.Hint("check it: tmskip status")
	if code != ExitOK {
		return exitWith(code, nil)
	}
	return nil
}

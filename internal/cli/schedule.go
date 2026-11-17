package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/launchd"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// pause waits between launchd polls; tests replace it.
var pause = time.Sleep

func (a *app) scheduleCommand() *cobra.Command {
	return &cobra.Command{
		Use:       "schedule [on|off]",
		Short:     "Turn the hourly LaunchAgent on or off; bare: show its state",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return a.runScheduleShow()
			}
			switch args[0] {
			case "on":
				return a.withLock(func() error {
					r, _, err := a.loadConfig()
					if err != nil {
						return err
					}
					return a.scheduleOn(r)
				})
			case "off":
				return a.withLock(a.scheduleOff)
			}
			return errorf(ExitError, "unknown argument %q — use \"schedule on\" or \"schedule off\"", args[0])
		},
	}
}

// withLock runs f holding the mutating-command lock.
func (a *app) withLock(f func() error) error {
	unlock, err := a.lock()
	if err != nil {
		return err
	}
	defer unlock()
	return f()
}

func (a *app) needLaunchd() error {
	if a.d.Launchd == nil {
		return errorf(ExitError, "launchd is not available")
	}
	return nil
}

// scheduleOn writes the agent plist and (re)loads it. The caller holds
// the lock. Reused by init.
func (a *app) scheduleOn(r *config.Resolved) error {
	if err := a.needLaunchd(); err != nil {
		return err
	}
	if r.Interval == 0 {
		return exitWith(ExitConfig, fmt.Errorf("set [schedule] interval in %s first", r.Path))
	}
	bin, warn, err := launchd.BinaryPath(a.d.Env)
	if err != nil {
		return exitWith(ExitError, err)
	}
	if warn != "" {
		a.out.Warn("%s", warn)
	}
	paths := a.statePaths()
	ag := launchd.Agent{Binary: bin, Config: r.Path, Interval: r.Interval, LogPath: paths.AgentLog}
	b, err := launchd.Render(ag)
	if err != nil {
		return exitWith(ExitError, err)
	}
	plist := launchd.PlistPath(a.d.Env.Home)
	fs := a.d.FS
	for _, dir := range []string{paths.LogDir, filepath.Dir(plist)} {
		if err := fs.MkdirAll(dir, 0o755); err != nil {
			return exitWith(ExitError, err)
		}
	}
	if err := fs.WriteFileAtomic(plist, b, 0o644); err != nil {
		return exitWith(ExitError, fmt.Errorf("writing %s: %w", plist, err))
	}
	loaded, err := a.d.Launchd.Loaded(launchd.Label)
	if err != nil {
		return exitWith(ExitError, err)
	}
	if loaded {
		if err := a.d.Launchd.Bootout(launchd.Label); err != nil {
			return exitWith(ExitError, err)
		}
		a.waitUnloaded()
	}
	if err := a.d.Launchd.Bootstrap(plist); err != nil {
		return exitWith(ExitError, err)
	}
	a.out.Line("Schedule on: %s sync every %s (%s)", bin, launchd.Interval(r.Interval), plist)
	a.log(fmt.Sprintf("schedule on every=%s binary=%s config=%s", launchd.Interval(r.Interval), bin, r.Path))
	a.out.Hint("check it later: tmskip status")
	return nil
}

// Bootout tears the job down asynchronously, and launchctl often rejects a
// bootstrap of the same label until it's gone (error 5 or 37). Wait for it,
// at most bootoutWaitSteps × bootoutWaitStep, then bootstrap anyway.
const (
	bootoutWaitStep  = 100 * time.Millisecond
	bootoutWaitSteps = 50
)

func (a *app) waitUnloaded() {
	for i := 0; i < bootoutWaitSteps; i++ {
		if loaded, err := a.d.Launchd.Loaded(launchd.Label); err != nil || !loaded {
			return
		}
		pause(bootoutWaitStep)
	}
}

// scheduleOff unloads the agent and removes its plist. The caller holds the
// lock. Reused by uninstall.
func (a *app) scheduleOff() error {
	if err := a.needLaunchd(); err != nil {
		return err
	}
	// The plist goes even if bootout fails, so launchd won't load the agent
	// again at the next login; the bootout error is still reported.
	bootErr := a.d.Launchd.Bootout(launchd.Label)
	if bootErr != nil {
		// launchctl can fail bootout for a job that isn't loaded (codes other
		// than 3/113); if launchd says it's not loaded, the schedule is off.
		if loaded, err := a.d.Launchd.Loaded(launchd.Label); err == nil && !loaded {
			bootErr = nil
		}
	}
	if err := a.d.FS.RemoveAll(launchd.PlistPath(a.d.Env.Home)); err != nil {
		return exitWith(ExitError, err)
	}
	if bootErr != nil {
		return exitWith(ExitError, fmt.Errorf("%w (the agent file is removed; it may still be loaded until you log out)", bootErr))
	}
	a.out.Line("Schedule off.")
	a.log("schedule off")
	a.out.Hint("turn it on: tmskip schedule on")
	return nil
}

func (a *app) runScheduleShow() error {
	if err := a.needLaunchd(); err != nil {
		return err
	}
	info := a.scheduleState(a.quietConfig())
	a.out.Line("Schedule: %s", info.text())
	if info.on {
		a.out.Hint("check it: tmskip doctor")
	} else {
		a.out.Hint("turn it on: tmskip schedule on")
	}
	return nil
}

// log appends lines to the run log; a failure is only a warning.
func (a *app) log(lines ...string) {
	if err := state.AppendLog(a.d.FS, a.statePaths(), a.d.Env.Now(), lines...); err != nil {
		a.out.Warn("writing the log: %v", err)
	}
}

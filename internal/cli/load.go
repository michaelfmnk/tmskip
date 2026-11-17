package cli

import (
	"errors"
	"fmt"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// configPath is the resolved config path (symlink kept).
func (a *app) configPath() string {
	return config.ResolvePath(a.d.Env, a.g.config, a.d.Env.Cwd)
}

func (a *app) statePaths() state.Paths { return state.PathsFor(a.d.Env.Home) }

// configErr maps config errors to exit 2, anything else to exit 1.
func configErr(err error) error {
	var me *config.MissingError
	var ie *config.InvalidError
	if errors.As(err, &me) || errors.As(err, &ie) {
		return exitWith(ExitConfig, err)
	}
	return exitWith(ExitError, err)
}

// loadConfig loads, validates and compiles the config, printing its
// warnings (always) and notes (-v).
func (a *app) loadConfig() (*config.Resolved, *config.File, error) {
	if a.d.Env.Home == "" {
		return nil, nil, errorf(ExitError, "HOME is not set")
	}
	path := a.configPath()
	f, err := config.Load(a.d.FS, path)
	if err != nil {
		return nil, nil, configErr(err)
	}
	r, warnings, err := config.Compile(f, path, a.d.Env, a.d.FS)
	if err != nil {
		return nil, nil, configErr(err)
	}
	for _, w := range warnings {
		a.out.Warn("%s", w.Text)
	}
	for _, n := range r.Notes {
		a.out.Verbose("note: %s", n)
	}
	return r, f, nil
}

// lock takes the mutating-command lock. A scheduled run that finds it held
// exits 0 silently.
func (a *app) lock() (func(), error) {
	if a.d.Env.Home == "" {
		return nil, errorf(ExitError, "HOME is not set")
	}
	unlock, ok, err := a.d.Locker.TryLock(a.statePaths().Lock)
	if err != nil {
		return nil, exitWith(ExitError, fmt.Errorf("taking the lock: %w", err))
	}
	if !ok {
		if a.d.Env.Scheduled() {
			return nil, exitWith(ExitOK, nil)
		}
		return nil, errorf(ExitError, "another tmskip run is in progress")
	}
	return unlock, nil
}

// humanBytes formats a size in decimal units, like Finder.
func humanBytes(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KB", "MB", "GB", "TB", "PB"}
	f := float64(n)
	i := -1
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}

package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/engine"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
)

const (
	keyFDADenied        = "fda-denied"
	keyConfigInvalid    = "config-invalid"
	keyConfigUnreadable = "config-unreadable"
	keyError            = "error"
)

// tccDBPath is the file a scheduled run tries to open to learn whether it
// has Full Disk Access (it opens only with access, whatever the config).
func tccDBPath(home string) string {
	return filepath.Join(home, "Library", "Application Support", "com.apple.TCC", "TCC.db")
}

// problemKeys names the problems of a run, for notification dedupe.
// A denied folder is covered by fda-denied; other unreadable folders are
// keyed by path.
func problemKeys(problems []engine.Problem, cfgErr, runErr error) []string {
	keys := map[string]bool{}
	for _, p := range problems {
		if p.Denied {
			keys[keyFDADenied] = true
		} else {
			keys["unreadable:"+p.Path] = true
		}
	}
	if cfgErr != nil {
		keys[keyConfigInvalid] = true
	}
	if runErr != nil {
		keys[keyError] = true
	}
	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// afterScheduledRun records the Full Disk Access probe and posts one
// notification for problems not notified before. notifyOn is the config's
// notify (a broken config notifies regardless: there is no setting to honour).
func (a *app) afterScheduledRun(keys []string, unreadable int, notifyOn bool) {
	a.afterScheduledRunForced(keys, unreadable, notifyOn, false)
}

// afterScheduledRunForced is afterScheduledRun; force notifies whatever
// notifyOn says (the config couldn't be read, so there's no setting).
func (a *app) afterScheduledRunForced(keys []string, unreadable int, notifyOn, force bool) {
	fs := a.d.FS
	paths := a.statePaths()
	st, _, err := state.Load(fs, paths)
	if err != nil {
		return
	}
	fda := "unknown"
	switch err := fs.Probe(tccDBPath(a.d.Env.Home)); {
	case err == nil:
		fda = "granted"
	case fsys.IsDenied(err):
		fda = "denied"
	}
	st.LastScheduled = &state.Scheduled{At: a.d.Env.Now(), FDA: fda}

	var fresh []string
	for _, k := range keys {
		if !slices.Contains(st.Notified, k) {
			fresh = append(fresh, k)
		}
	}
	if len(fresh) > 0 && a.d.Notifier != nil && (notifyOn || force) {
		if err := a.d.Notifier.Notify("tmskip", notificationBody(fresh, unreadable)); err != nil {
			a.out.Warn("notification failed: %v", err)
		}
	}
	st.Notified = keys
	if err := state.Save(fs, paths, st); err != nil {
		a.out.Warn("saving state: %v", err)
	}
}

func notificationBody(fresh []string, unreadable int) string {
	switch {
	case slices.Contains(fresh, keyConfigInvalid):
		return `tmskip's config is missing or invalid, so scheduled runs do nothing. Run "tmskip doctor".`
	case slices.Contains(fresh, keyConfigUnreadable) && slices.Contains(fresh, keyFDADenied):
		return `tmskip can't read its config — Full Disk Access is off for the scheduled run. Run "tmskip doctor".`
	case slices.Contains(fresh, keyConfigUnreadable):
		return `tmskip can't read its config, so scheduled runs do nothing. Run "tmskip doctor".`
	case slices.Contains(fresh, keyFDADenied):
		return `tmskip can't read some folders — Full Disk Access is off for the scheduled run. Run "tmskip doctor".`
	case slices.Contains(fresh, keyError):
		return `tmskip's scheduled run failed. Run "tmskip status".`
	}
	return fmt.Sprintf(`tmskip couldn't read %s. Run "tmskip status".`, plural(unreadable, "folder"))
}

// afterFailedRun: a scheduled run that couldn't finish records it in state
// and notifies. A config that is missing, invalid or unreadable notifies
// whatever notify says (no setting can be honoured; an EPERM is a Full Disk
// Access denial); a later failure honours notifyOn. Manual runs just return
// the error.
func (a *app) afterFailedRun(err error, notifyOn bool) error {
	if !a.d.Env.Scheduled() || a.d.Env.Home == "" {
		return err
	}
	code := ExitError
	var ee *exitError
	if errors.As(err, &ee) {
		code = ee.code
	}
	var keys []string
	force := false
	var re *config.ReadError
	switch {
	case code == ExitConfig:
		keys, force = []string{keyConfigInvalid}, true
	case errors.As(err, &re):
		keys, force = []string{keyConfigUnreadable}, true
		if fsys.IsDenied(re.Err) {
			keys = append(keys, keyFDADenied)
		}
	default:
		keys = []string{keyError}
	}
	paths := a.statePaths()
	if st, _, lerr := state.Load(a.d.FS, paths); lerr == nil {
		st.LastRun = &state.Run{At: a.d.Env.Now(), Scheduled: true, ExitCode: code, Error: err.Error(), Problems: []state.RunProblem{}}
		if serr := state.Save(a.d.FS, paths, st); serr != nil {
			a.out.Warn("saving state: %v", serr)
		}
	}
	sort.Strings(keys)
	a.afterScheduledRunForced(keys, 0, notifyOn, force)
	return err
}

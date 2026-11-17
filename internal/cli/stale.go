package cli

import (
	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/launchd"
)

const staleFix = `run "tmskip schedule on" to update`

// writesAgent: commands that (re)write or remove the agent don't warn that
// it is stale.
func writesAgent(cmd *cobra.Command, args []string) bool {
	switch cmd.Name() {
	case "schedule":
		return len(args) == 1
	case "init", "uninstall", "help":
		return true
	}
	return false
}

// warnStale prints each stale-agent finding as a warning. Never in a
// scheduled run (spec: it may not be able to start at all).
func (a *app) warnStale() {
	for _, s := range a.staleAgent() {
		a.out.Warn("%s — %s", s, staleFix)
	}
}

// staleAgent compares the installed agent with what `schedule on` would
// install now. Empty when there is no agent, or in a scheduled run.
func (a *app) staleAgent() []string {
	if a.staleDone {
		return a.stale
	}
	a.staleDone = true
	if a.d.Env.Scheduled() || a.d.Launchd == nil || a.d.Env.Home == "" {
		return nil
	}
	b, err := a.d.FS.ReadFile(launchd.PlistPath(a.d.Env.Home))
	if err != nil {
		return nil
	}
	have, err := launchd.Parse(b)
	if err != nil {
		a.stale = []string{"the agent file " + launchd.PlistPath(a.d.Env.Home) + " can't be read"}
		return a.stale
	}
	want := have
	want.Config = a.configPath()
	if bin, _, err := launchd.BinaryPath(a.d.Env); err == nil {
		want.Binary = bin
	}
	if r := a.quietConfig(); r != nil {
		want.Interval = r.Interval
	}
	a.stale = launchd.Stale(a.d.FS, have, want)
	return a.stale
}

// quietConfig loads the config without printing anything; nil if it can't.
func (a *app) quietConfig() *config.Resolved {
	path := a.configPath()
	f, err := config.Load(a.d.FS, path)
	if err != nil {
		return nil
	}
	r, _, err := config.Compile(f, path, a.d.Env, a.d.FS)
	if err != nil {
		return nil
	}
	return r
}

// scheduleState reports the LaunchAgent: loaded or not, its interval, and
// staleness. Unknown without a launchd.
func (a *app) scheduleState(r *config.Resolved) scheduleInfo {
	if a.d.Launchd == nil || a.d.Env.Home == "" {
		return scheduleInfo{}
	}
	info := scheduleInfo{known: true}
	loaded, err := a.d.Launchd.Loaded(launchd.Label)
	if err != nil {
		a.out.Warn("can't ask launchd about the agent: %v", err)
	}
	info.on = loaded
	b, err := a.d.FS.ReadFile(launchd.PlistPath(a.d.Env.Home))
	present := err == nil
	if present {
		if ag, err := launchd.Parse(b); err == nil && ag.Interval > 0 {
			info.interval = launchd.Interval(ag.Interval)
		}
	}
	if present && !loaded {
		info.detail = `agent file present but not loaded — run "tmskip schedule on"`
	}
	info.stale = a.staleAgent()
	return info
}

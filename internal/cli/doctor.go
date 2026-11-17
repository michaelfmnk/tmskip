package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/launchd"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func (a *app) doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check config, Time Machine, Full Disk Access and the agent",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runDoctor() },
	}
}

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"` // ok | warn | fail | info
	Detail string `json:"detail"`
	Fix    string `json:"fix"`
}

const fdaSettingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

func (a *app) runDoctor() error {
	var checks []check
	add := func(c check) { checks = append(checks, c) }
	configInvalid := false

	// config
	path := a.configPath()
	var r *config.Resolved
	var file *config.File
	f, err := config.Load(a.d.FS, path)
	if err == nil {
		file = f
		var warnings []config.Warning
		r, warnings, err = config.Compile(f, path, a.d.Env, a.d.FS)
		if err == nil {
			c := check{Name: "config", Status: "ok", Detail: fmt.Sprintf("%s — %s, %s", path, plural(len(r.Roots), "root"), plural(len(r.Rules), "enabled rule"))}
			if len(warnings) > 0 {
				var ws []string
				for _, w := range warnings {
					ws = append(ws, w.Text)
				}
				c.Status, c.Detail, c.Fix = "warn", strings.Join(ws, "; "), "fix the paths in "+path
			}
			add(c)
		}
	}
	if err != nil {
		configInvalid = true
		fix := "fix " + path
		var me *config.MissingError
		if errors.As(err, &me) {
			fix = "create one: tmskip init"
		}
		add(check{Name: "config", Status: "fail", Detail: err.Error(), Fix: fix})
	}

	add(a.timeMachineCheck())

	st, _, _ := state.Load(a.d.FS, a.statePaths())
	add(a.fdaCheck(r, st))

	for _, c := range a.agentChecks(r) {
		add(c)
	}

	for _, g := range config.Groups() {
		c := check{Name: "group " + g.Name, Status: "info"}
		if file == nil {
			c.Detail = "config unavailable"
		} else {
			switch config.GroupStatus(g, file) {
			case "present":
				c.Detail = "in your config"
			case "changed":
				c.Detail = "changed in your config"
				c.Fix = "compare: tmskip config defaults " + g.Name
			default:
				c.Detail = "not in your config"
				c.Fix = "tmskip config add " + g.Name
			}
		}
		add(c)
	}
	add(check{Name: "version", Status: "info", Detail: "tmskip " + a.d.Version})

	failed := ""
	for _, c := range checks {
		if c.Status == "fail" && failed == "" {
			failed = c.Fix
		}
	}
	if failed != "" {
		a.out.Hint(failed)
	} else {
		a.out.Hint("all good — status: tmskip status")
	}
	if a.g.json {
		a.out.JSON(map[string]any{"checks": checks})
	} else {
		for _, c := range checks {
			status := c.Status
			switch c.Status {
			case "ok":
				status = a.out.green(fmt.Sprintf("%-4s", status))
			case "warn":
				status = a.out.yellow(fmt.Sprintf("%-4s", status))
			case "fail":
				status = a.out.red(fmt.Sprintf("%-4s", status))
			default:
				status = fmt.Sprintf("%-4s", status)
			}
			a.out.Line("%s  %-22s %s", status, c.Name, c.Detail)
			if c.Fix != "" && (c.Status == "fail" || c.Status == "warn") {
				a.out.Line("      %-22s fix: %s", "", c.Fix)
			}
		}
	}
	switch {
	case configInvalid:
		return exitWith(ExitConfig, nil)
	case failed != "":
		return exitWith(ExitError, nil)
	}
	return nil
}

func (a *app) timeMachineCheck() check {
	c := check{Name: "time machine"}
	if a.d.Runner == nil {
		c.Status, c.Detail = "info", "not checked"
		return c
	}
	stdout, stderr, code, err := a.d.Runner.Run("/usr/bin/tmutil", "destinationinfo")
	out := string(stdout) + string(stderr)
	switch {
	case err != nil:
		c.Status, c.Detail = "warn", "couldn't run tmutil: "+err.Error()
	case strings.Contains(out, "No destinations configured"):
		c.Status, c.Detail = "warn", "no backup destination configured — exclusions have no effect yet"
		c.Fix = "add a backup disk in System Settings → General → Time Machine"
	case code != 0:
		c.Status, c.Detail = "warn", fmt.Sprintf("tmutil destinationinfo failed (exit %d): %s", code, strings.TrimSpace(out))
	default:
		c.Status, c.Detail = "ok", "a backup destination is configured"
	}
	return c
}

// agentBinary is the binary the agent runs (or would run).
func (a *app) agentBinary() string {
	if b, err := a.d.FS.ReadFile(launchd.PlistPath(a.d.Env.Home)); err == nil {
		if ag, err := launchd.Parse(b); err == nil {
			return ag.Binary
		}
	}
	bin, _, _ := launchd.BinaryPath(a.d.Env)
	return bin
}

func (a *app) fdaCheck(r *config.Resolved, st *state.State) check {
	c := check{Name: "full disk access", Status: "info"}
	var needed []string
	if r != nil {
		needed = needsFDA(r)
	}
	need := "not needed (no root or fixed path is in a protected folder)"
	if len(needed) > 0 {
		need = "needed for " + strings.Join(needed, ", ")
	}
	fix := fmt.Sprintf("grant Full Disk Access to %s in System Settings → Privacy & Security → Full Disk Access (open %q)", a.agentBinary(), fdaSettingsURL)
	if st == nil || st.LastScheduled == nil {
		c.Detail = "no scheduled run yet, so not known; " + need
		return c
	}
	switch st.LastScheduled.FDA {
	case "granted":
		c.Status, c.Detail = "ok", "granted to the scheduled run; "+need
	case "denied":
		c.Detail = "denied to the scheduled run; " + need
		if len(needed) > 0 {
			c.Status, c.Fix = "fail", fix
		}
	default:
		c.Detail = "unknown (last scheduled run couldn't tell); " + need
	}
	return c
}

func (a *app) agentChecks(r *config.Resolved) []check {
	if a.d.Launchd == nil {
		return []check{{Name: "agent", Status: "info", Detail: "not checked"}}
	}
	fix := "run \"tmskip schedule on\" to update"
	plist := launchd.PlistPath(a.d.Env.Home)
	b, err := a.d.FS.ReadFile(plist)
	present := err == nil
	loaded, lerr := a.d.Launchd.Loaded(launchd.Label)
	agent := check{Name: "agent", Status: "ok"}
	switch {
	case lerr != nil:
		agent.Status, agent.Detail = "warn", "can't ask launchd: "+lerr.Error()
	case !present && !loaded:
		agent.Status, agent.Detail, agent.Fix = "info", "schedule is off", "tmskip schedule on"
		return []check{agent}
	case !present:
		agent.Status, agent.Detail, agent.Fix = "fail", "loaded, but its file "+plist+" is gone", "tmskip schedule on"
		return []check{agent}
	case !loaded:
		agent.Status, agent.Detail, agent.Fix = "fail", "agent file present but not loaded", "tmskip schedule on"
	default:
		agent.Detail = "loaded (" + launchd.Label + ")"
	}
	checks := []check{agent}
	have, err := launchd.Parse(b)
	if err != nil {
		return append(checks, check{Name: "agent binary", Status: "fail", Detail: plist + " can't be read: " + err.Error(), Fix: fix})
	}
	bin, _, berr := launchd.BinaryPath(a.d.Env)
	c := check{Name: "agent binary", Status: "ok", Detail: have.Binary}
	if !fsys.Exists(a.d.FS, have.Binary) {
		c.Status, c.Detail, c.Fix = "fail", have.Binary+" is missing", fix
	} else if berr == nil && have.Binary != bin {
		c.Status, c.Detail, c.Fix = "fail", fmt.Sprintf("the agent runs %s, but this is %s", have.Binary, bin), fix
	}
	checks = append(checks, c)
	c = check{Name: "agent config", Status: "ok", Detail: have.Config}
	if want := a.configPath(); have.Config != want {
		c.Status, c.Detail, c.Fix = "fail", fmt.Sprintf("the agent uses %s, but this run uses %s", have.Config, want), fix
	}
	checks = append(checks, c)
	c = check{Name: "agent interval", Status: "ok", Detail: "every " + launchd.Interval(have.Interval)}
	switch {
	case r == nil:
		c.Status, c.Detail = "info", "every "+launchd.Interval(have.Interval)+" (config unavailable)"
	case r.Interval == 0:
		c.Status, c.Detail, c.Fix = "fail", "the agent runs every "+launchd.Interval(have.Interval)+", but the config has no [schedule] interval", "set [schedule] interval in "+r.Path
	case have.Interval != r.Interval:
		c.Status, c.Detail, c.Fix = "fail", fmt.Sprintf("the agent runs every %s, but the config says %s", launchd.Interval(have.Interval), launchd.Interval(r.Interval)), fix
	}
	return append(checks, c)
}

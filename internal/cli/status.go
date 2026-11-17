package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/launchd"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func (a *app) statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Last run, managed folders and size, problems, schedule",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runStatus() },
	}
}

// runBare is `tmskip` with no command: status, or a pointer to init.
func (a *app) runBare() error {
	if a.d.Env.Home != "" {
		var me *config.MissingError
		if _, err := config.Load(a.d.FS, a.configPath()); errors.As(err, &me) && me.Target == "" {
			a.out.Line(`no config yet — run "tmskip init"`)
			a.out.Hint(hintGetStarted)
			return exitWith(ExitConfig, nil)
		}
	}
	return a.runStatus()
}

type managedJSON struct {
	Count      int   `json:"count"`
	Bytes      int64 `json:"bytes"`
	Unmeasured int   `json:"unmeasured"`
}

type scheduleJSON struct {
	On       bool     `json:"on"`
	Interval string   `json:"interval"`
	Stale    []string `json:"stale"`
}

type statusJSON struct {
	Config   string        `json:"config"`
	LastRun  *state.Run    `json:"last_run"`
	Managed  managedJSON   `json:"managed"`
	Problems []jsonProblem `json:"problems"`
	Schedule scheduleJSON  `json:"schedule"`
	Warnings []string      `json:"warnings"`
}

// scheduleInfo is what tmskip knows about the LaunchAgent.
type scheduleInfo struct {
	known    bool
	on       bool
	interval string // the agent's interval, when installed
	detail   string // e.g. "agent file present but not loaded"
	stale    []string
}

func (a *app) runStatus() error {
	r, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	st, warn, err := state.Load(a.d.FS, a.statePaths())
	if err != nil {
		return exitWith(ExitError, err)
	}
	if warn != "" {
		a.out.Warn("%s", warn)
	}
	sched := a.scheduleState(r)
	if sched.interval == "" && r.Interval > 0 {
		sched.interval = launchd.Interval(r.Interval)
	}

	m := managedJSON{Count: len(st.Owned)}
	for _, o := range st.Owned {
		if o.Bytes == nil {
			m.Unmeasured++
		} else {
			m.Bytes += *o.Bytes
		}
	}
	var problems []state.RunProblem
	if st.LastRun != nil {
		problems = st.LastRun.Problems
	}
	if sched.on {
		a.out.Hint("details: tmskip list --sizes")
	} else {
		a.out.Hint("run hourly: tmskip schedule on")
	}

	if a.g.json {
		doc := statusJSON{Config: r.Path, LastRun: st.LastRun, Managed: m, Problems: []jsonProblem{},
			Schedule: scheduleJSON{On: sched.on, Interval: sched.interval, Stale: nonNil(sched.stale)},
			Warnings: nonNil(a.out.warnings)}
		for _, p := range problems {
			doc.Problems = append(doc.Problems, jsonProblem(p))
		}
		a.out.JSON(doc)
		return nil
	}

	row := func(label, value string) { a.out.Line("%-11s%s", label, value) }
	row("Config", r.Path)
	if st.LastRun == nil {
		row("Last run", "never")
	} else {
		lr := st.LastRun
		kind := "manual"
		if lr.Scheduled {
			kind = "scheduled"
		}
		row("Last run", fmt.Sprintf("%s %s — +%d −%d =%d, exit %d", lr.At.Format("2006-01-02 15:04"), kind, lr.Excluded, lr.Included, lr.Unchanged, lr.ExitCode))
		if lr.Error != "" {
			row("", "error: "+lr.Error)
		}
	}
	managed := plural(m.Count, "folder")
	if m.Count > 0 {
		managed += ", " + humanBytes(m.Bytes) + " (size when excluded"
		if m.Unmeasured > 0 {
			managed += fmt.Sprintf("; %d not measured", m.Unmeasured)
		}
		managed += ")"
	}
	row("Managed", managed)
	if len(problems) == 0 {
		row("Problems", "none")
	} else {
		row("Problems", plural(len(problems), "folder")+" couldn't be read")
		for _, p := range problems {
			a.out.Line("  ! %s: %s", p.Path, p.Error)
		}
	}
	row("Schedule", sched.text())
	return nil
}

func (s scheduleInfo) text() string {
	switch {
	case !s.known:
		return "unknown"
	case s.on && s.interval != "":
		return "on, every " + s.interval
	case s.on:
		return "on"
	case s.detail != "":
		return "off (" + s.detail + ")"
	}
	return "off"
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

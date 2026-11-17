package launchd

import (
	"fmt"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// Stale compares the installed agent (have) with what this run would
// install (want). One message per difference; empty means up to date.
func Stale(fs fsys.FS, have, want Agent) []string {
	var out []string
	if !fsys.Exists(fs, have.Binary) { // a dangling symlink can't be run either
		out = append(out, fmt.Sprintf("the agent's binary %s is missing", have.Binary))
	} else if have.Binary != want.Binary {
		out = append(out, fmt.Sprintf("the agent runs %s, but this is %s", have.Binary, want.Binary))
	}
	if have.Config != want.Config {
		out = append(out, fmt.Sprintf("the agent uses config %s, but this run uses %s", have.Config, want.Config))
	}
	if have.Interval != want.Interval {
		if want.Interval == 0 {
			out = append(out, fmt.Sprintf("the agent runs every %s, but the config has no [schedule] interval", Interval(have.Interval)))
		} else {
			out = append(out, fmt.Sprintf("the agent runs every %s, but the config says %s", Interval(have.Interval), Interval(want.Interval)))
		}
	}
	return out
}

// Interval formats a duration the way configs write it: "1h", "30m".
func Interval(d time.Duration) string {
	switch {
	case d > 0 && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d > 0 && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

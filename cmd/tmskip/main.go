// Command tmskip keeps rebuildable developer folders out of Time Machine.
package main

import (
	"os"

	"github.com/michaelfmnk/tmskip/internal/cli"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/launchd"
	"github.com/michaelfmnk/tmskip/internal/notify"
	"github.com/michaelfmnk/tmskip/internal/runner"
	"github.com/michaelfmnk/tmskip/internal/state"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// version is set at release time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	env := sysenv.Real()
	run := runner.Exec{}
	os.Exit(cli.Run(os.Args[1:], cli.Deps{
		Env:      env,
		Version:  version,
		FS:       fsys.OS{},
		Locker:   state.FlockLocker{},
		Launchd:  launchd.New(run, env.Uid),
		Notifier: notify.Osascript{R: run},
		Runner:   run,
	}))
}

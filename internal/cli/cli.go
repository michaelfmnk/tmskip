// Package cli is tmskip's command-line interface: cobra commands, global
// flags, output rendering, hints, prompts and exit-code mapping. It is the
// only package that formats output.
package cli

import (
	"bufio"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/launchd"
	"github.com/michaelfmnk/tmskip/internal/notify"
	"github.com/michaelfmnk/tmskip/internal/runner"
	"github.com/michaelfmnk/tmskip/internal/state"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// Deps are the real-world dependencies, injected so tests can fake them.
// cmd/tmskip/main.go is the only place real implementations are built.
type Deps struct {
	Env      sysenv.Env
	Version  string // from -ldflags -X main.version
	FS       fsys.FS
	Locker   state.Locker
	Launchd  launchd.Launchd
	Notifier notify.Notifier
	Runner   runner.Runner // tmutil destinationinfo, open (System Settings)
}

type globals struct {
	config  string
	json    bool
	verbose bool
	quiet   bool
}

// app is one invocation: the dependencies, parsed global flags and output.
type app struct {
	d   Deps
	g   *globals
	out *output

	stale     []string // stale-agent findings, computed once per run
	staleDone bool
	stdin     *bufio.Reader // for prompts
}

// Run executes tmskip with args (without argv[0]) and returns the exit code.
func Run(args []string, d Deps) int {
	if d.Env.Getenv == nil {
		d.Env.Getenv = func(string) string { return "" }
	}
	g := &globals{}
	a := &app{d: d, g: g, out: newOutput(d.Env, g)}

	if d.Env.Euid == 0 {
		a.out.Errorf("tmskip refuses to run as root")
		return ExitError
	}

	root := a.rootCommand()
	root.SetArgs(args)
	root.SetOut(d.Env.Stdout)
	root.SetErr(d.Env.Stderr)
	root.SetIn(orEmpty(d.Env.Stdin))
	err := root.Execute()
	return a.finish(err)
}

func orEmpty(r io.Reader) io.Reader {
	if r == nil {
		return eofReader{}
	}
	return r
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// finish prints the error (if any) and the hint, and maps err to an exit code.
func (a *app) finish(err error) int {
	code := ExitOK
	if err != nil {
		code = ExitError
		var ee *exitError
		if errors.As(err, &ee) {
			code = ee.code
			if ee.err != nil {
				a.out.Errorf("%s", ee.err)
			}
		} else {
			a.out.Errorf("%s", err)
		}
	}
	switch code {
	case ExitConfig:
		if a.out.hint != hintGetStarted {
			a.out.Hint("fix the config, then: tmskip doctor")
		}
	case ExitUnreadable:
		a.out.Hint("see what couldn't be read: tmskip status")
	case ExitError:
		// keep a hint only if the command chose one for this failure
	}
	a.out.flushHint()
	return code
}

const hintGetStarted = "get started: tmskip init"

func (a *app) rootCommand() *cobra.Command {
	var showVersion bool
	root := &cobra.Command{
		Use:           "tmskip",
		Short:         "Keep rebuildable developer folders out of Time Machine",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				_, err := io.WriteString(a.d.Env.Stdout, "tmskip "+a.d.Version+"\n")
				return err
			}
			return a.runBare()
		},
	}
	root.CompletionOptions.DisableDefaultCmd = true
	pf := root.PersistentFlags()
	pf.StringVar(&a.g.config, "config", "", "config file (default ~/.config/tmskip/config.toml)")
	pf.BoolVar(&a.g.json, "json", false, "JSON output (status, list, why, doctor, sync)")
	pf.BoolVarP(&a.g.verbose, "verbose", "v", false, "more detail")
	pf.BoolVarP(&a.g.quiet, "quiet", "q", false, "errors and warnings only")
	root.Flags().BoolVar(&showVersion, "version", false, "print the version and exit")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// Flags are parsed now; refresh the output mode.
		a.out = newOutput(a.d.Env, a.g)
		if showVersion || writesAgent(cmd, args) {
			return
		}
		a.warnStale()
	}
	root.AddCommand(a.syncCommand(), a.statusCommand(), a.listCommand(), a.whyCommand(), a.scheduleCommand(), a.doctorCommand(), a.configCommand(), a.uninstallCommand(), a.initCommand())
	return root
}

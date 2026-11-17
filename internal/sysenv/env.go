// Package sysenv is the only place tmskip reads its process environment:
// $HOME, environment variables, user IDs, argv[0], the terminal and the clock.
package sysenv

import (
	"io"
	"os"
	"os/exec"
	"time"

	"golang.org/x/term"
)

// Env is everything tmskip learns from the process it runs in.
type Env struct {
	Home      string              // $HOME; empty → error "HOME is not set"
	Getenv    func(string) string // TMSKIP_CONFIG, XDG_CONFIG_HOME, NO_COLOR, TMPDIR, TMSKIP_AGENT
	Euid, Uid int
	Args0     string // os.Args[0], for the agent's binary path
	Cwd       string // working directory, for relative --config / TMSKIP_CONFIG
	LookPath  func(string) (string, error)
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	StdoutTTY bool
	StdinTTY  bool
	StderrTTY bool       // progress line only on an interactive stderr
	TermWidth func() int // stderr terminal width in columns; 0 if unknown
	Now       func() time.Time
}

// Scheduled reports whether this run was started by the LaunchAgent.
func (e Env) Scheduled() bool {
	return e.Getenv != nil && e.Getenv("TMSKIP_AGENT") == "1"
}

// Real returns the environment of the current process.
func Real() Env {
	cwd, _ := os.Getwd()
	args0 := ""
	if len(os.Args) > 0 {
		args0 = os.Args[0]
	}
	return Env{
		Home:      os.Getenv("HOME"),
		Getenv:    os.Getenv,
		Euid:      os.Geteuid(),
		Uid:       os.Getuid(),
		Args0:     args0,
		Cwd:       cwd,
		LookPath:  exec.LookPath,
		Stdin:     os.Stdin,
		Stdout:    os.Stdout,
		Stderr:    os.Stderr,
		StdoutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		StdinTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		StderrTTY: term.IsTerminal(int(os.Stderr.Fd())),
		TermWidth: func() int {
			w, _, err := term.GetSize(int(os.Stderr.Fd()))
			if err != nil {
				return 0
			}
			return w
		},
		Now: time.Now,
	}
}

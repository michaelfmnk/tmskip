// Package runner runs subprocesses (launchctl, tmutil, osascript, open)
// behind an interface so tests never spawn real ones.
package runner

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// Runner runs a program. A non-zero exit is reported in exitCode, not err;
// err means the program couldn't be run at all.
type Runner interface {
	Run(name string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

// Exec is the real Runner.
type Exec struct{}

func (Exec) Run(name string, args ...string) ([]byte, []byte, int, error) {
	var out, errb bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.Bytes(), errb.Bytes(), ee.ExitCode(), nil
	}
	if err != nil {
		return nil, nil, -1, err
	}
	return out.Bytes(), errb.Bytes(), 0, nil
}

// Result is a scripted Fake result.
type Result struct {
	Stdout, Stderr string
	Code           int
	Err            error
}

// Fake records calls and returns scripted results, looked up by the full
// command line ("name arg1 arg2"), then by name alone; default: exit 0.
type Fake struct {
	Calls   [][]string
	Results map[string]Result
}

func (f *Fake) Run(name string, args ...string) ([]byte, []byte, int, error) {
	f.Calls = append(f.Calls, append([]string{name}, args...))
	r, ok := f.Results[strings.Join(append([]string{name}, args...), " ")]
	if !ok {
		r = f.Results[name]
	}
	return []byte(r.Stdout), []byte(r.Stderr), r.Code, r.Err
}

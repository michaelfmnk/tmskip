package cli

import "fmt"

// Exit codes (spec).
const (
	ExitOK         = 0
	ExitError      = 1
	ExitConfig     = 2
	ExitUnreadable = 3
)

// exitError carries an exit code out of a command. A nil err means "exit
// with this code without printing anything more" (the command already
// reported, or must stay silent).
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err == nil {
		return fmt.Sprintf("exit %d", e.code)
	}
	return e.err.Error()
}

func (e *exitError) Unwrap() error { return e.err }

func exitWith(code int, err error) error { return &exitError{code: code, err: err} }

func errorf(code int, format string, args ...any) error {
	return &exitError{code: code, err: fmt.Errorf(format, args...)}
}

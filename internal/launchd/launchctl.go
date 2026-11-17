package launchd

import (
	"fmt"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/runner"
)

// Launchd loads and unloads the agent in the user's GUI domain.
type Launchd interface {
	Bootstrap(plistPath string) error // launchctl bootstrap gui/<uid> <plist>
	Bootout(label string) error       // launchctl bootout gui/<uid>/<label>; "not loaded" → nil
	Loaded(label string) (bool, error)
}

const launchctl = "/bin/launchctl" // absolute: launchd jobs get a minimal PATH

// New returns the launchctl-backed Launchd for uid.
func New(r runner.Runner, uid int) Launchd { return &ctl{r: r, domain: fmt.Sprintf("gui/%d", uid)} }

type ctl struct {
	r      runner.Runner
	domain string
}

func (l *ctl) run(args ...string) (int, error) {
	_, stderr, code, err := l.r.Run(launchctl, args...)
	if err != nil {
		return code, fmt.Errorf("launchctl %s: %w", args[0], err)
	}
	if code != 0 && strings.TrimSpace(string(stderr)) != "" {
		return code, fmt.Errorf("launchctl %s: %s", args[0], strings.TrimSpace(string(stderr)))
	}
	if code != 0 {
		return code, fmt.Errorf("launchctl %s: exit %d", args[0], code)
	}
	return 0, nil
}

func (l *ctl) Bootstrap(plistPath string) error {
	_, err := l.run("bootstrap", l.domain, plistPath)
	return err
}

func (l *ctl) Bootout(label string) error {
	code, err := l.run("bootout", l.domain+"/"+label)
	if code == 3 || code == 113 { // no such process / not found: not loaded
		return nil
	}
	return err
}

func (l *ctl) Loaded(label string) (bool, error) {
	code, err := l.run("print", l.domain+"/"+label)
	switch code {
	case 0:
		return true, nil
	case 113:
		return false, nil
	}
	return false, err
}

// Fake is an in-memory Launchd for tests.
type Fake struct {
	IsLoaded     bool
	Calls        []string
	BootstrapErr error
}

func (f *Fake) Bootstrap(p string) error {
	f.Calls = append(f.Calls, "bootstrap "+p)
	if f.BootstrapErr != nil {
		return f.BootstrapErr
	}
	f.IsLoaded = true
	return nil
}

func (f *Fake) Bootout(label string) error {
	f.Calls = append(f.Calls, "bootout "+label)
	f.IsLoaded = false
	return nil
}

func (f *Fake) Loaded(string) (bool, error) { return f.IsLoaded, nil }

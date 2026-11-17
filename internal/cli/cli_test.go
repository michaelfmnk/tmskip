package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

var fixedTime = time.Date(2026, 11, 16, 10, 0, 0, 0, time.FixedZone("CET", 3600))

func fixedClock() time.Time { return fixedTime }

func mapGetenv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func newTestDeps(t *testing.T) (Deps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	env := sysenv.Env{
		Home:   "/Users/t",
		Getenv: mapGetenv(nil),
		Euid:   501,
		Uid:    501,
		Args0:  "/opt/homebrew/bin/tmskip",
		Cwd:    "/Users/t",
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
		Now:    fixedClock,
	}
	return Deps{Env: env, Version: "1.2.3"}, &stdout, &stderr
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestRefusesRoot(t *testing.T) {
	d, _, stderr := newTestDeps(t)
	d.Env.Euid = 0
	if code := Run([]string{"status"}, d); code != ExitError {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "tmskip refuses to run as root") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestUnknownCommandIsError(t *testing.T) {
	d, _, _ := newTestDeps(t)
	if code := Run([]string{"frobnicate"}, d); code != ExitError {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestVersionFlag(t *testing.T) {
	d, stdout, _ := newTestDeps(t)
	if code := Run([]string{"--version"}, d); code != ExitOK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if stdout.String() != "tmskip 1.2.3\n" {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestQuietSuppressesHint(t *testing.T) {
	d, stdout, _ := newTestDeps(t)
	o := newOutput(d.Env, &globals{quiet: true})
	o.Line("hello")
	o.Hint("do the thing")
	o.flushHint()
	if stdout.Len() != 0 {
		t.Fatalf("quiet output = %q", stdout.String())
	}

	d, stdout, _ = newTestDeps(t)
	o = newOutput(d.Env, &globals{})
	o.Line("hello")
	o.Hint("do the thing")
	o.flushHint()
	if got := lastLine(stdout.String()); got != "→ do the thing" {
		t.Fatalf("last line = %q", got)
	}
}

func TestJSONSuppressesHint(t *testing.T) {
	d, stdout, _ := newTestDeps(t)
	o := newOutput(d.Env, &globals{json: true})
	o.JSON(map[string]int{"a": 1})
	o.Hint("do the thing")
	o.flushHint()
	if strings.Contains(stdout.String(), "→") {
		t.Fatalf("json output has hint: %q", stdout.String())
	}
	if strings.TrimSpace(stdout.String()) != `{"a":1}` && !strings.Contains(stdout.String(), `"a": 1`) {
		t.Fatalf("json output = %q", stdout.String())
	}
}

func TestNoColorWhenNotTTYOrNO_COLOR(t *testing.T) {
	cases := []struct {
		name    string
		tty     bool
		noColor string
		want    bool
	}{
		{"not a tty", false, "", false},
		{"NO_COLOR set", true, "1", false},
		{"tty", true, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, stdout, stderr := newTestDeps(t)
			d.Env.StdoutTTY = c.tty
			d.Env.Getenv = mapGetenv(map[string]string{"NO_COLOR": c.noColor})
			o := newOutput(d.Env, &globals{})
			o.Line(o.green("+") + " x")
			o.Errorf("boom")
			o.Hint("next")
			o.flushHint()
			got := strings.Contains(stdout.String()+stderr.String(), "\x1b[")
			if got != c.want {
				t.Fatalf("escape present = %v, want %v (%q)", got, c.want, stdout.String())
			}
		})
	}
}

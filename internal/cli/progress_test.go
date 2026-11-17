package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type manualClock struct{ t time.Time }

func (c *manualClock) now() time.Time          { return c.t }
func (c *manualClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTestProgress(width int) (*progressLine, *bytes.Buffer, *manualClock) {
	var buf bytes.Buffer
	clk := &manualClock{t: fixedTime}
	return &progressLine{w: &buf, now: clk.now, width: width, home: home}, &buf, clk
}

func TestProgressDrawsOneLineInPlace(t *testing.T) {
	p, buf, _ := newTestProgress(80)
	p.Scanning(home+"/Developer/app", 3, 1)
	if got := buf.String(); got != "\rScanning ~/Developer/app  3 folders · 1 found\x1b[K" {
		t.Fatalf("got %q", got)
	}
	buf.Reset()
	p.Clear()
	if got := buf.String(); got != "\r\x1b[K" {
		t.Fatalf("clear = %q", got)
	}
	buf.Reset()
	p.Clear() // nothing shown: nothing written
	if buf.Len() != 0 {
		t.Fatalf("second clear wrote %q", buf.String())
	}
}

func TestProgressThrottlesToTenPerSecond(t *testing.T) {
	p, buf, clk := newTestProgress(80)
	p.Scanning(home+"/a", 1, 0)
	n := strings.Count(buf.String(), "\r")
	clk.advance(50 * time.Millisecond)
	p.Scanning(home+"/b", 2, 0)
	if strings.Count(buf.String(), "\r") != n {
		t.Fatalf("redrew within 50ms: %q", buf.String())
	}
	clk.advance(60 * time.Millisecond)
	p.Measured(1, 9)
	if !strings.HasSuffix(buf.String(), "\rMeasuring sizes  1/9\x1b[K") {
		t.Fatalf("no redraw after 110ms: %q", buf.String())
	}
}

func TestProgressTruncatesToWidth(t *testing.T) {
	p, buf, _ := newTestProgress(30)
	p.Scanning(home+"/Developer/a/very/long/шлях/that/would/wrap", 12345, 678)
	line := strings.TrimSuffix(strings.TrimPrefix(buf.String(), "\r"), "\x1b[K")
	if n := utf8.RuneCountInString(line); n > 29 || !strings.HasSuffix(line, "…") {
		t.Fatalf("line %q (%d runes)", line, n)
	}
	// Unknown width falls back to 80.
	p2, buf2, _ := newTestProgress(0)
	p2.Scanning(home+"/"+strings.Repeat("x", 200), 1, 0)
	line = strings.TrimSuffix(strings.TrimPrefix(buf2.String(), "\r"), "\x1b[K")
	if utf8.RuneCountInString(line) != 79 {
		t.Fatalf("fallback width: %d", utf8.RuneCountInString(line))
	}
}

func TestProgressShownOnlyOnInteractiveStderr(t *testing.T) {
	cases := []struct {
		name  string
		tty   bool
		vars  map[string]string
		args  []string
		shown bool
	}{
		{"sync on a tty", true, nil, []string{"sync"}, true},
		{"sync -n on a tty", true, nil, []string{"sync", "-n"}, true},
		{"list --sizes on a tty", true, nil, []string{"list", "--sizes"}, true},
		{"stderr not a tty", false, nil, []string{"sync"}, false},
		{"-q", true, nil, []string{"sync", "-q"}, false},
		{"--json", true, nil, []string{"sync", "-n", "--json"}, false},
		{"scheduled", true, map[string]string{"TMSKIP_AGENT": "1"}, []string{"sync", "-q", "--config", cfgPath}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.writeConfig(nodeConfig)
			e.mkdirs(dev + "/app/node_modules/x/")
			e.mustRun(0, "sync") // so list has something to measure
			e.stderrTTY = c.tty
			for k, v := range c.vars {
				e.vars[k] = v
			}
			e.mustRun(0, c.args...)
			got := strings.Contains(e.stderr.String(), "\r")
			if got != c.shown {
				t.Fatalf("progress shown = %v, want %v: %q", got, c.shown, e.stderr.String())
			}
			if c.shown && !strings.HasSuffix(e.stderr.String(), "\r\x1b[K") {
				t.Fatalf("progress line not cleared: %q", e.stderr.String())
			}
		})
	}
}

func TestSyncProgressPhases(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/x/")
	e.stderrTTY = true
	e.mustRun(0, "sync", "-n")
	s := e.stderr.String()
	scan, measure := strings.Index(s, "Scanning ~/Developer"), strings.Index(s, "Measuring sizes  1/1")
	if scan < 0 || measure < scan {
		t.Fatalf("stderr %q", s)
	}
}

// The init preview header comes after the scan, directly above the plan.
func TestInitPreviewHeaderAfterScan(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/")
	e.combined = true
	e.stderrTTY = true
	interactive(e, "n", "n")
	e.mustRun(0, "init")
	out := e.stdout.String()
	header := strings.Index(out, "Preview (nothing is changed yet):")
	lastClear := strings.LastIndex(out[:header], "\r\x1b[K")
	scan := strings.Index(out, "Scanning ")
	plan := strings.Index(out, "+ "+dev+"/app/node_modules")
	if scan < 0 || header < 0 || lastClear < scan || header < lastClear || plan < header {
		t.Fatalf("order wrong:\n%q", out)
	}
}

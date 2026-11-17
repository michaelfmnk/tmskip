package cli

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/state"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

var update = flag.Bool("update", false, "rewrite golden files")

const (
	home    = "/Users/t"
	dev     = "/Users/t/Developer"
	cfgPath = "/Users/t/.config/tmskip/config.toml"
)

// testEnv is a fake machine: memfs, fake lock, captured output.
type testEnv struct {
	t     *testing.T
	fs    *memfs.FS
	lock  *state.FakeLocker
	vars  map[string]string
	stdin string
	tty   bool
	euid  int
	// stderrTTY: stderr is a terminal (progress line); combined: stderr is
	// written into the stdout buffer, to check ordering.
	stderrTTY bool
	combined  bool
	stdout    bytes.Buffer
	stderr    bytes.Buffer
	deps      func(*Deps) // last-minute tweaks
}

func newEnv(t *testing.T, opts ...memfs.Option) *testEnv {
	t.Helper()
	m := memfs.New(opts...)
	m.Mkdirs(home)
	return &testEnv{t: t, fs: m, lock: &state.FakeLocker{}, vars: map[string]string{}, euid: 501}
}

func (e *testEnv) Deps() Deps {
	var stderr io.Writer = &e.stderr
	if e.combined {
		stderr = &e.stdout
	}
	env := sysenv.Env{
		Home:      home,
		Getenv:    func(k string) string { return e.vars[k] },
		Euid:      e.euid,
		Uid:       501,
		Args0:     "/opt/homebrew/bin/tmskip",
		Cwd:       home,
		LookPath:  func(string) (string, error) { return "/opt/homebrew/bin/tmskip", nil },
		Stdin:     strings.NewReader(e.stdin),
		Stdout:    &e.stdout,
		Stderr:    stderr,
		StdoutTTY: e.tty,
		StdinTTY:  e.tty,
		StderrTTY: e.stderrTTY,
		TermWidth: func() int { return 80 },
		Now:       fixedClock,
	}
	d := Deps{Env: env, Version: "1.2.3", FS: e.fs, Locker: e.lock}
	if e.deps != nil {
		e.deps(&d)
	}
	return d
}

// run executes tmskip with args, resetting captured output first.
func (e *testEnv) run(args ...string) int {
	e.stdout.Reset()
	e.stderr.Reset()
	return Run(args, e.Deps())
}

func (e *testEnv) writeConfig(src string) {
	e.fs.WriteFile(cfgPath, []byte(src))
}

func (e *testEnv) mkdirs(paths ...string) {
	for _, p := range paths {
		if strings.HasSuffix(p, "/") {
			e.fs.Mkdirs(strings.TrimSuffix(p, "/"))
		} else {
			e.fs.WriteFile(p, []byte("x"))
		}
	}
}

func (e *testEnv) excluded(p string) bool {
	_, ok := e.fs.Xattrs(p)[exclusion.AttrName]
	return ok
}

func (e *testEnv) marked(p string) bool {
	_, ok := e.fs.Xattrs(p)[exclusion.MarkerName]
	return ok
}

func (e *testEnv) loadState() *state.State {
	e.t.Helper()
	s, _, err := state.Load(e.fs, state.PathsFor(home))
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *testEnv) mustRun(want int, args ...string) {
	e.t.Helper()
	if code := e.run(args...); code != want {
		e.t.Fatalf("tmskip %v → exit %d, want %d\nstdout:\n%s\nstderr:\n%s", args, code, want, e.stdout.String(), e.stderr.String())
	}
}

const nodeConfig = `version = 1
roots = ["~/Developer"]
skip = [".git"]
keep = []
notify = true

[schedule]
interval = "1h"

[[rule]]
name = "node"
names = ["node_modules"]
`

// golden compares got with testdata/<name>, rewriting it under -update.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	p := filepath.Join("testdata", name)
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(p, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", p, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s mismatch\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

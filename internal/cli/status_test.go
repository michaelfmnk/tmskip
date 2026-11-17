package cli

import (
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/state"
)

func TestBareNoConfig(t *testing.T) {
	e := newEnv(t)
	e.mustRun(2)
	if !strings.Contains(e.stdout.String(), `run "tmskip init"`) {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
	if got := lastLine(e.stdout.String()); got != "→ get started: tmskip init" {
		t.Fatalf("hint %q", got)
	}
}

func TestBareWithConfigIsStatus(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	e.mustRun(0)
	bare := e.stdout.String()
	e.mustRun(0, "status")
	if bare != e.stdout.String() || !strings.Contains(bare, "Last run") {
		t.Fatalf("bare:\n%s\nstatus:\n%s", bare, e.stdout.String())
	}
}

func TestStatusNeverRun(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	e.mustRun(0, "status")
	out := e.stdout.String()
	for _, want := range []string{"Config     " + cfgPath, "Last run   never", "Managed    0 folders", "Schedule   unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestStatusAfterSync(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.fs.WriteFile(dev+"/a/node_modules/f", make([]byte, 1_000_000))
	e.fs.WriteFile(dev+"/b/node_modules/f", make([]byte, 1_000_000))
	e.mkdirs(dev + "/locked/")
	e.fs.Deny(dev+"/locked", syscall.EPERM)
	e.mustRun(3, "sync")
	// A folder found later by its marker alone has no measured size.
	st := e.loadState()
	st.Owned = append(st.Owned, state.Owned{Path: dev + "/c/node_modules"})
	state.Save(e.fs, state.PathsFor(home), st)

	e.mustRun(0, "status")
	out := e.stdout.String()
	for _, want := range []string{
		"Last run   2026-11-16 10:00 manual — +2 −0 =0, exit 3",
		"Managed    3 folders, 2.0 MB (size when excluded; 1 not measured)",
		"Problems   1 folder couldn't be read",
		"! " + dev + "/locked: operation not permitted",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	e.mustRun(0, "status", "--json")
	golden(t, "status.golden.json", e.stdout.Bytes())
}

func TestStatusHint(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	e.mustRun(0, "status")
	if got := lastLine(e.stdout.String()); got != "→ run hourly: tmskip schedule on" {
		t.Fatalf("hint %q", got)
	}
}

func TestReadOnlyCommandsTakeNoLock(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/")
	e.lock.Held = true
	for _, args := range [][]string{{"status"}, {"list"}, {"why", dev + "/app/node_modules"}, {"sync", "-n"}} {
		e.mustRun(0, args...)
	}
	if e.lock.Taken != 0 {
		t.Fatal("a read-only command took the lock")
	}
}

package cli

import (
	"errors"
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/launchd"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// orderLaunchd records how many xattr ops had happened when bootout ran.
type orderLaunchd struct {
	launchd.Fake
	fs           *memfs.FS
	opsAtBootout int
}

func (o *orderLaunchd) Bootout(label string) error {
	o.opsAtBootout = len(o.fs.Ops())
	return o.Fake.Bootout(label)
}

func installed(t *testing.T) *testEnv {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/a/node_modules/", dev+"/b/node_modules/")
	e.mustRun(0, "sync")
	e.installAgent(goodAgent())
	return e
}

func TestUninstallOrder(t *testing.T) {
	e := installed(t)
	e.fs.ResetOps()
	o := &orderLaunchd{fs: e.fs}
	o.IsLoaded = true
	prev := e.deps
	e.deps = func(d *Deps) { prev(d); d.Launchd = o }
	e.mustRun(0, "uninstall")
	if o.opsAtBootout != 0 || len(o.Calls) != 1 {
		t.Fatalf("bootout after %d xattr ops; calls %v", o.opsAtBootout, o.Calls)
	}
	if _, err := e.fs.Lstat(plistPath); err == nil {
		t.Fatal("plist left")
	}
	ops := e.fs.Ops()
	if len(ops) != 4 || !strings.HasPrefix(ops[0], "rm com.apple") || !strings.HasPrefix(ops[1], "rm dev.tmskip.managed") {
		t.Fatalf("ops = %v", ops)
	}
	if got := lastLine(e.stdout.String()); got != "→ remove the binary: brew uninstall tmskip" {
		t.Fatalf("hint %q", got)
	}
}

func TestUninstallRemovesStateDirKeepsConfigAndLogs(t *testing.T) {
	e := installed(t)
	e.mustRun(0, "uninstall")
	for _, p := range []string{dev + "/a/node_modules", dev + "/b/node_modules"} {
		if e.marked(p) || e.excluded(p) {
			t.Fatalf("%s still owned", p)
		}
	}
	paths := state.PathsFor(home)
	if _, err := e.fs.Lstat(paths.Dir); err == nil {
		t.Fatal("state dir left")
	}
	if _, err := e.fs.Lstat(cfgPath); err != nil {
		t.Fatal("config removed")
	}
	if log, err := e.fs.ReadFile(paths.Log); err != nil || !strings.Contains(string(log), "uninstall") {
		t.Fatalf("log: %q %v", log, err)
	}
	if e.lock.Locked {
		t.Fatal("lock not released")
	}
}

func TestUninstallKeepsStateWhenSomethingUnreadable(t *testing.T) {
	e := installed(t)
	e.fs.Deny(dev+"/b/node_modules", syscall.EPERM) // its marker can't be read
	e.mustRun(3, "uninstall")
	if e.marked(dev+"/a/node_modules") || !e.marked(dev+"/b/node_modules") {
		t.Fatal("wrong folders re-included")
	}
	st := e.loadState()
	if len(st.Owned) != 1 || st.Owned[0].Path != dev+"/b/node_modules" {
		t.Fatalf("owned = %+v", st.Owned)
	}
}

func TestUninstallBrokenConfigWarns(t *testing.T) {
	e := installed(t)
	e.writeConfig("version = 9\n")
	e.mustRun(0, "uninstall")
	if !strings.Contains(e.stderr.String(), "config unusable — sweeping only folders recorded in state") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	if e.marked(dev+"/a/node_modules") || e.excluded(dev+"/b/node_modules") {
		t.Fatal("hint folders not re-included")
	}
}

func TestUninstallTakesLock(t *testing.T) {
	e := installed(t)
	e.lock.Held = true
	e.mustRun(1, "uninstall")
	if !e.marked(dev + "/a/node_modules") {
		t.Fatal("ran while locked")
	}
}

// failingBootout is a loaded agent that launchctl can't boot out.
type failingBootout struct{ launchd.Fake }

func (f *failingBootout) Bootout(string) error {
	f.Calls = append(f.Calls, "bootout")
	return errors.New("launchctl bootout: Boot-out failed: 5: Input/output error")
}

func withFailingBootout(e *testEnv) *failingBootout {
	f := &failingBootout{}
	f.IsLoaded = true
	prev := e.deps
	e.deps = func(d *Deps) { prev(d); d.Launchd = f }
	return f
}

// Review R1: the plist must go even when bootout fails, or the agent comes
// back at the next login.
func TestScheduleOffRemovesPlistWhenBootoutFails(t *testing.T) {
	e := installed(t)
	withFailingBootout(e)
	e.mustRun(1, "schedule", "off")
	if _, err := e.fs.Lstat(plistPath); err == nil {
		t.Fatal("plist left in LaunchAgents")
	}
	if !strings.Contains(e.stderr.String(), "Input/output error") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
}

// Review R1: uninstall must not report success (and drop state) while the
// agent may still be loaded.
func TestUninstallScheduleOffFailureKeepsState(t *testing.T) {
	e := installed(t)
	withFailingBootout(e)
	code := e.run("uninstall")
	if code == 0 {
		t.Fatalf("exit 0\nstdout:\n%s\nstderr:\n%s", e.stdout.String(), e.stderr.String())
	}
	if _, err := e.fs.Lstat(plistPath); err == nil {
		t.Fatal("plist left in LaunchAgents")
	}
	if _, err := e.fs.Lstat(state.PathsFor(home).File); err != nil {
		t.Fatal("state removed although uninstall didn't finish")
	}
	if !strings.Contains(e.stderr.String()+e.stdout.String(), `run "tmskip uninstall" again`) {
		t.Fatalf("no rerun advice:\n%s\n%s", e.stdout.String(), e.stderr.String())
	}
	if e.marked(dev + "/a/node_modules") {
		t.Fatal("sweep skipped")
	}
	if got := lastLine(e.stdout.String()); got != "→ try again: tmskip uninstall" {
		t.Fatalf("hint %q", got)
	}
}

// Review round 2, N2: launchctl may fail bootout for a job that isn't
// loaded (e.g. "5: Input/output error"); if launchd says it's not loaded,
// the schedule is off and uninstall completes.
func TestUninstallBootoutErrorButNotLoaded(t *testing.T) {
	e := installed(t)
	f := withFailingBootout(e)
	f.IsLoaded = false
	e.mustRun(0, "uninstall")
	if _, err := e.fs.Lstat(state.PathsFor(home).Dir); err == nil {
		t.Fatal("state kept")
	}
	if _, err := e.fs.Lstat(plistPath); err == nil {
		t.Fatal("plist left")
	}
}

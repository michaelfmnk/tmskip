package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/state"
)

func TestSyncExcludesAndPrints(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.fs.WriteFile(dev+"/app/node_modules/x/index.js", make([]byte, 10000))
	e.mustRun(0, "sync")
	out := e.stdout.String()
	if !strings.Contains(out, "+ "+dev+"/app/node_modules  (node") {
		t.Fatalf("stdout:\n%s", out)
	}
	if !strings.Contains(out, "+1 −0 =0") {
		t.Fatalf("summary missing:\n%s", out)
	}
	if !e.marked(dev+"/app/node_modules") || !e.excluded(dev+"/app/node_modules") {
		t.Fatal("xattrs not set")
	}
	st := e.loadState()
	if len(st.Owned) != 1 || st.Owned[0].Path != dev+"/app/node_modules" || st.Owned[0].Bytes == nil || *st.Owned[0].Bytes == 0 {
		t.Fatalf("state = %+v", st.Owned)
	}
	if st.LastRun == nil || st.LastRun.Excluded != 1 || st.LastRun.Scheduled || st.LastRun.ExitCode != 0 {
		t.Fatalf("last run = %+v", st.LastRun)
	}
	if e.lock.Taken != 1 || e.lock.Locked {
		t.Fatalf("lock = %+v", e.lock)
	}
}

func TestSyncDryRunChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.fs.WriteFile(dev+"/app/node_modules/x/index.js", make([]byte, 2_000_000))
	e.mustRun(0, "sync", "-n")
	if len(e.fs.Ops()) != 0 {
		t.Fatalf("dry run wrote xattrs: %v", e.fs.Ops())
	}
	if _, err := e.fs.Lstat(state.PathsFor(home).File); err == nil {
		t.Fatal("dry run wrote state")
	}
	if _, err := e.fs.Lstat(state.PathsFor(home).Log); err == nil {
		t.Fatal("dry run wrote the log")
	}
	if !strings.Contains(e.stdout.String(), "2.0 MB") {
		t.Fatalf("no human size:\n%s", e.stdout.String())
	}
	if e.lock.Taken != 0 {
		t.Fatal("dry run took the lock")
	}
}

func TestSyncReincludesAfterRuleDisabled(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/")
	e.mustRun(0, "sync")
	e.writeConfig(strings.Replace(nodeConfig, `names = ["node_modules"]`, "names = [\"node_modules\"]\nenabled = false", 1))
	e.mustRun(0, "sync")
	if !strings.Contains(e.stdout.String(), "- "+dev+"/app/node_modules  (no rule matches)") {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
	if e.marked(dev+"/app/node_modules") || e.excluded(dev+"/app/node_modules") {
		t.Fatal("xattrs left")
	}
	if st := e.loadState(); len(st.Owned) != 0 || st.LastRun.Included != 1 {
		t.Fatalf("state = %+v %+v", st.Owned, st.LastRun)
	}
}

func TestSyncUnreadableExits3(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/locked/", dev+"/app/node_modules/")
	e.fs.Deny(dev+"/locked", syscall.EPERM)
	e.mustRun(3, "sync")
	if !strings.Contains(e.stderr.String(), dev+"/locked: operation not permitted") {
		t.Fatalf("stderr:\n%s", e.stderr.String())
	}
	if !e.excluded(dev + "/app/node_modules") {
		t.Fatal("readable sibling not excluded")
	}
	if lastLine(e.stdout.String()) != "→ see what couldn't be read: tmskip status" {
		t.Fatalf("hint: %q", lastLine(e.stdout.String()))
	}
	st := e.loadState()
	if st.LastRun.ExitCode != 3 || len(st.LastRun.Problems) != 1 || !st.LastRun.Problems[0].Denied {
		t.Fatalf("last run = %+v", st.LastRun)
	}
}

func TestSyncInvalidConfigExits2(t *testing.T) {
	e := newEnv(t)
	e.writeConfig("version = 1\nroots = [\"Developer\"]\n")
	e.mustRun(2, "sync")
	if !strings.Contains(e.stderr.String(), `roots: "Developer" is relative`) {
		t.Fatalf("stderr:\n%s", e.stderr.String())
	}
	if lastLine(e.stdout.String()) != "→ fix the config, then: tmskip doctor" {
		t.Fatalf("hint: %q", e.stdout.String())
	}
	e2 := newEnv(t)
	e2.mustRun(2, "sync")
	if !strings.Contains(e2.stderr.String(), `run "tmskip init"`) {
		t.Fatalf("missing config stderr:\n%s", e2.stderr.String())
	}
}

func TestSyncLockHeldManual(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.lock.Held = true
	e.mustRun(1, "sync")
	if !strings.Contains(e.stderr.String(), "another tmskip run is in progress") {
		t.Fatalf("stderr:\n%s", e.stderr.String())
	}
}

func TestSyncLockHeldScheduledIsSilent(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.lock.Held = true
	e.vars["TMSKIP_AGENT"] = "1"
	e.mustRun(0, "sync", "-q", "--config", cfgPath)
	if e.stdout.Len()+e.stderr.Len() != 0 {
		t.Fatalf("output: %q %q", e.stdout.String(), e.stderr.String())
	}
}

func TestSyncJSONGolden(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(strings.Replace(nodeConfig, `roots = ["~/Developer"]`, `roots = ["~/Developer", "~/Develper"]`, 1))
	e.fs.WriteFile(dev+"/my app/node_modules/a.js", make([]byte, 5000))
	e.fs.WriteFile(dev+"/Проєкти/web/node_modules/b.js", make([]byte, 9000))
	e.mkdirs(dev + "/old/vendor/")
	e.fs.SetXattr(dev+"/old/vendor", "dev.tmskip.managed", []byte("1"))
	e.mkdirs(dev + "/locked/")
	e.fs.Deny(dev+"/locked", syscall.EACCES)
	e.mustRun(3, "sync", "-n", "--json")
	var v map[string]any
	if err := json.Unmarshal(e.stdout.Bytes(), &v); err != nil {
		t.Fatalf("not one JSON document: %v\n%s", err, e.stdout.String())
	}
	if strings.Contains(e.stdout.String(), "→") {
		t.Fatal("hint in JSON output")
	}
	golden(t, "sync_dry_run.golden.json", e.stdout.Bytes())
}

func TestSyncVerboseShowsKeepAndUserOwned(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/a/node_modules/", dev+"/b/node_modules/")
	e.mustRun(0, "sync")
	e.mkdirs(dev + "/c/node_modules/")
	e.fs.SetXattr(dev+"/c/node_modules", "com.apple.metadata:com_apple_backup_excludeItem", []byte("x"))
	e.mustRun(0, "sync")
	if strings.Contains(e.stdout.String(), "= ") || strings.Contains(e.stdout.String(), "· ") {
		t.Fatalf("= or · lines without -v:\n%s", e.stdout.String())
	}
	e.mustRun(0, "sync", "-v")
	out := e.stdout.String()
	if !strings.Contains(out, "= "+dev+"/a/node_modules  (node)") || !strings.Contains(out, "· "+dev+"/c/node_modules  (excluded by you, not by tmskip)") {
		t.Fatalf("verbose:\n%s", out)
	}
	if e.marked(dev + "/c/node_modules") {
		t.Fatal("touched a user exclusion")
	}
}

func TestSyncHint(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	e.mustRun(0, "sync", "-n")
	if got := lastLine(e.stdout.String()); got != "→ apply: tmskip sync" {
		t.Fatalf("dry-run hint %q", got)
	}
	e.mustRun(0, "sync")
	if got := lastLine(e.stdout.String()); got != "→ see what's managed: tmskip list" {
		t.Fatalf("sync hint %q", got)
	}
	e.mustRun(0, "sync", "-q")
	if e.stdout.Len() != 0 {
		t.Fatalf("-q output %q", e.stdout.String())
	}
}

func TestSyncWritesLog(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/app/node_modules/", dev+"/locked/")
	e.fs.Deny(dev+"/locked", syscall.EPERM)
	e.mustRun(3, "sync")
	log, err := e.fs.ReadFile(state.PathsFor(home).Log)
	if err != nil {
		t.Fatal(err)
	}
	want := "2026-11-16T10:00:00+01:00 sync manual exit=3 +1 -0 =0 problems=1\n" +
		"2026-11-16T10:00:00+01:00 + " + dev + "/app/node_modules (node)\n" +
		"2026-11-16T10:00:00+01:00 ! " + dev + "/locked: operation not permitted\n"
	if string(log) != want {
		t.Fatalf("log =\n%s\nwant\n%s", log, want)
	}
}

func TestSyncWarnsMissingRootEvenQuiet(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(strings.Replace(nodeConfig, `"~/Developer"`, `"~/Develper"`, 1))
	e.mustRun(0, "sync", "-q")
	if !strings.Contains(e.stderr.String(), "warning: root ~/Develper does not exist") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
}

// Review R3: an unreadable keep/root parent makes sync report a read
// problem (exit 3), never "invalid config" (exit 2); EPERM is a denial.
func TestSyncUnreadableConfiguredPathExits3(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		e := newEnv(t)
		e.writeConfig(strings.Replace(nodeConfig, "keep = []", `keep = ["~/Private/x"]`, 1))
		e.mkdirs(dev+"/a/node_modules/", home+"/Private/x/")
		e.fs.DenySearch(home+"/Private", errno)
		e.mustRun(3, "sync", "--json")
		var v struct {
			Problems []struct {
				Path   string
				Denied bool
			}
		}
		if err := json.Unmarshal(e.stdout.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if len(v.Problems) != 1 || v.Problems[0].Path != home+"/Private/x" || v.Problems[0].Denied != (errno == syscall.EPERM) {
			t.Fatalf("%v: problems = %+v", errno, v.Problems)
		}
		// Review round 2, N1: with an unresolvable configured path nothing is applied.
		if e.excluded(dev+"/a/node_modules") || len(e.fs.Ops()) != 0 {
			t.Fatalf("changed something: %v", e.fs.Ops())
		}
	}
}

// Review round 2, N1: an unresolvable keep/root/fixed path stops the run
// from changing anything, says so, and keeps the state's hints; sync -n
// shows the same problems and an empty plan.
func TestSyncUnresolvableConfigChangesNothing(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/old/node_modules/", home+"/Locked/")
	e.mustRun(0, "sync")
	owned := e.loadState().Owned
	e.writeConfig(strings.Replace(nodeConfig, "keep = []", `keep = ["~/Locked/app/secret"]`, 1))
	e.mkdirs(dev + "/new/node_modules/")
	e.fs.RemoveAll(dev + "/old/node_modules") // would drop its hint
	e.fs.DenySearch(home+"/Locked", syscall.EPERM)
	e.fs.ResetOps()

	e.mustRun(3, "sync", "-n", "--json")
	var v struct {
		Actions  []any
		Problems []struct{ Denied bool }
	}
	if err := json.Unmarshal(e.stdout.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Actions) != 0 || len(v.Problems) != 1 || !v.Problems[0].Denied {
		t.Fatalf("dry run = %s", e.stdout.String())
	}

	e.mustRun(3, "sync")
	if !strings.Contains(e.stderr.String(), "nothing was changed") || !strings.Contains(e.stderr.String(), home+"/Locked/app/secret") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	if len(e.fs.Ops()) != 0 || e.excluded(dev+"/new/node_modules") {
		t.Fatalf("ops %v", e.fs.Ops())
	}
	if st := e.loadState(); !reflect.DeepEqual(st.Owned, owned) || st.LastRun.ExitCode != 3 {
		t.Fatalf("owned %+v (was %+v), last run %+v", st.Owned, owned, st.LastRun)
	}
}

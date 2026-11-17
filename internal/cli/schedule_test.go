package cli

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michaelfmnk/tmskip/internal/launchd"
)

var plistPath = launchd.PlistPath(home)

// withLaunchd gives the env a fake launchd and the binary the agent runs.
func withLaunchd(e *testEnv) *launchd.Fake {
	f := &launchd.Fake{}
	e.fs.WriteFile("/opt/homebrew/bin/tmskip", []byte("bin"))
	prev := e.deps
	e.deps = func(d *Deps) {
		if prev != nil {
			prev(d)
		}
		d.Launchd = f
	}
	return f
}

func (e *testEnv) installAgent(a launchd.Agent) {
	b, err := launchd.Render(a)
	if err != nil {
		e.t.Fatal(err)
	}
	e.fs.WriteFile(plistPath, b)
}

func goodAgent() launchd.Agent {
	return launchd.Agent{Binary: "/opt/homebrew/bin/tmskip", Config: cfgPath, Interval: time.Hour, LogPath: home + "/Library/Logs/tmskip/agent.log"}
}

func TestScheduleOnWritesPlistAndBootstraps(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mustRun(0, "schedule", "on")
	b, err := e.fs.ReadFile(plistPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := launchd.Parse(b)
	if err != nil || got != goodAgent() {
		t.Fatalf("plist agent = %+v %v", got, err)
	}
	if !reflect.DeepEqual(f.Calls, []string{"bootstrap " + plistPath}) {
		t.Fatalf("calls = %v", f.Calls)
	}
	if _, err := e.fs.Lstat(home + "/Library/Logs/tmskip"); err != nil {
		t.Fatal("logs dir not created")
	}
	if got := lastLine(e.stdout.String()); got != "→ check it later: tmskip status" {
		t.Fatalf("hint %q", got)
	}
}

func TestScheduleOnKeepsConfigSymlink(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.fs.WriteFile(home+"/dotfiles/tmskip.toml", []byte(nodeConfig))
	e.fs.Symlink(home+"/dotfiles/tmskip.toml", cfgPath)
	e.mustRun(0, "schedule", "on")
	b, _ := e.fs.ReadFile(plistPath)
	if a, _ := launchd.Parse(b); a.Config != cfgPath {
		t.Fatalf("config = %q, want the symlink", a.Config)
	}
}

func TestScheduleOnReloadsWhenLoaded(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	f.IsLoaded = true
	e.writeConfig(nodeConfig)
	e.mustRun(0, "schedule", "on")
	if !reflect.DeepEqual(f.Calls, []string{"bootout " + launchd.Label, "bootstrap " + plistPath}) {
		t.Fatalf("calls = %v", f.Calls)
	}
}

func TestScheduleOnNeedsInterval(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	e.writeConfig(strings.Replace(nodeConfig, "[schedule]\ninterval = \"1h\"\n", "", 1))
	e.mustRun(2, "schedule", "on")
	if !strings.Contains(e.stderr.String(), "set [schedule] interval in "+cfgPath+" first") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	if len(f.Calls) != 0 {
		t.Fatalf("calls = %v", f.Calls)
	}
}

func TestScheduleOnWarnsTempBinary(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	prev := e.deps
	e.deps = func(d *Deps) { prev(d); d.Env.Args0 = "/private/var/folders/xy/T/go-build1/exe/tmskip" }
	e.mustRun(0, "schedule", "on")
	if !strings.Contains(e.stderr.String(), "temporary folder") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
}

func TestScheduleOff(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	f.IsLoaded = true
	e.writeConfig(nodeConfig)
	e.installAgent(goodAgent())
	e.mustRun(0, "schedule", "off")
	if !reflect.DeepEqual(f.Calls, []string{"bootout " + launchd.Label}) {
		t.Fatalf("calls = %v", f.Calls)
	}
	if _, err := e.fs.Lstat(plistPath); err == nil {
		t.Fatal("plist left")
	}
	if got := lastLine(e.stdout.String()); got != "→ turn it on: tmskip schedule on" {
		t.Fatalf("hint %q", got)
	}
}

func TestScheduleBareState(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	e.writeConfig(nodeConfig)

	e.mustRun(0, "schedule")
	if !strings.Contains(e.stdout.String(), "off") || lastLine(e.stdout.String()) != "→ turn it on: tmskip schedule on" {
		t.Fatalf("off:\n%s", e.stdout.String())
	}
	e.installAgent(goodAgent())
	e.mustRun(0, "schedule")
	if !strings.Contains(e.stdout.String(), `agent file present but not loaded — run "tmskip schedule on"`) {
		t.Fatalf("present not loaded:\n%s", e.stdout.String())
	}
	f.IsLoaded = true
	e.mustRun(0, "schedule")
	if !strings.Contains(e.stdout.String(), "on, every 1h") || lastLine(e.stdout.String()) != "→ check it: tmskip doctor" {
		t.Fatalf("on:\n%s", e.stdout.String())
	}
	if len(f.Calls) != 0 {
		t.Fatalf("bare schedule changed launchd: %v", f.Calls)
	}
	e.mustRun(1, "schedule", "sideways")
}

func TestScheduleTakesLock(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.lock.Held = true
	e.mustRun(1, "schedule", "on")
	e.mustRun(1, "schedule", "off")
	if len(f.Calls) != 0 {
		t.Fatalf("calls = %v", f.Calls)
	}
	e.lock.Held = false
	e.mustRun(0, "schedule", "on")
	if e.lock.Taken != 1 {
		t.Fatalf("lock taken %d times", e.lock.Taken)
	}
}

func TestStaleWarningOnManualCommand(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	f.IsLoaded = true
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	a := goodAgent()
	a.Interval = 2 * time.Hour
	e.installAgent(a)
	e.mustRun(0, "status")
	if !strings.Contains(e.stderr.String(), `the agent runs every 2h, but the config says 1h — run "tmskip schedule on" to update`) {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	if !strings.Contains(e.stdout.String(), "Schedule   on, every 2h") {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
	e.mustRun(0, "status", "--json")
	if !strings.Contains(e.stdout.String(), `"stale": [`) || !strings.Contains(e.stdout.String(), "every 2h") {
		t.Fatalf("json:\n%s", e.stdout.String())
	}
	// schedule on itself doesn't warn (it fixes it).
	e.mustRun(0, "schedule", "on")
	if strings.Contains(e.stderr.String(), "to update") {
		t.Fatalf("schedule on warned: %q", e.stderr.String())
	}
}

func TestNoStaleCheckWhenScheduled(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	a := goodAgent()
	a.Binary = "/gone/tmskip"
	e.installAgent(a)
	e.vars["TMSKIP_AGENT"] = "1"
	e.mustRun(0, "sync", "-q", "--config", cfgPath)
	if e.stderr.Len() != 0 {
		t.Fatalf("stderr %q", e.stderr.String())
	}
}

func TestSyncNeverTouchesAgent(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/")
	e.installAgent(goodAgent())
	e.mustRun(0, "sync")
	if len(f.Calls) != 0 {
		t.Fatalf("calls = %v", f.Calls)
	}
	if b, _ := e.fs.ReadFile(plistPath); !strings.Contains(string(b), "<integer>3600</integer>") {
		t.Fatal("plist changed")
	}
}

// slowBootout is a launchd whose bootout finishes asynchronously: the job
// stays loaded for a few more Loaded() calls.
type slowBootout struct {
	launchd.Fake
	stillLoaded int // Loaded() calls that still report true after bootout
	log         []string
}

func (s *slowBootout) Bootout(label string) error {
	s.log = append(s.log, "bootout")
	return nil
}

func (s *slowBootout) Loaded(string) (bool, error) {
	if s.stillLoaded > 0 {
		s.stillLoaded--
		s.log = append(s.log, "loaded")
		return true, nil
	}
	s.log = append(s.log, "unloaded")
	return false, nil
}

func (s *slowBootout) Bootstrap(p string) error {
	s.log = append(s.log, "bootstrap")
	return nil
}

func withPause(t *testing.T) *int {
	n := 0
	prev := pause
	pause = func(time.Duration) { n++ }
	t.Cleanup(func() { pause = prev })
	return &n
}

// Review R7: bootstrap right after bootout is often rejected by launchctl;
// wait (bounded) until the old job is gone.
func TestScheduleOnWaitsForBootout(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	s := &slowBootout{stillLoaded: 4} // 1 for the "is it loaded?" check, 3 while it winds down
	prev := e.deps
	e.deps = func(d *Deps) { prev(d); d.Launchd = s }
	e.writeConfig(nodeConfig)
	pauses := withPause(t)
	e.mustRun(0, "schedule", "on")
	want := []string{"loaded", "bootout", "loaded", "loaded", "loaded", "unloaded", "bootstrap"}
	if !reflect.DeepEqual(s.log, want) {
		t.Fatalf("calls = %v", s.log)
	}
	if *pauses != 3 {
		t.Fatalf("paused %d times", *pauses)
	}
}

func TestScheduleOnBootoutWaitIsBounded(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	s := &slowBootout{stillLoaded: 1 << 30}
	prev := e.deps
	e.deps = func(d *Deps) { prev(d); d.Launchd = s }
	e.writeConfig(nodeConfig)
	pauses := withPause(t)
	e.run("schedule", "on")
	if *pauses == 0 || *pauses > 100 || s.log[len(s.log)-1] != "bootstrap" {
		t.Fatalf("paused %d times; last call %q", *pauses, s.log[len(s.log)-1])
	}
}

package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/runner"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func withRunner(e *testEnv, results map[string]runner.Result) *runner.Fake {
	r := &runner.Fake{Results: results}
	prev := e.deps
	e.deps = func(d *Deps) {
		if prev != nil {
			prev(d)
		}
		d.Runner = r
	}
	return r
}

var tmDest = map[string]runner.Result{"/usr/bin/tmutil destinationinfo": {Stdout: "====\nName          : Backup\nKind          : Local\n"}}

type checkJSON struct {
	Name, Status, Detail, Fix string
}

func doctorChecks(t *testing.T, e *testEnv, want int) []checkJSON {
	t.Helper()
	e.mustRun(want, "doctor", "--json")
	var doc struct{ Checks []checkJSON }
	if err := json.Unmarshal(e.stdout.Bytes(), &doc); err != nil {
		t.Fatalf("%v\n%s", err, e.stdout.String())
	}
	return doc.Checks
}

func findCheck(checks []checkJSON, name string) checkJSON {
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	return checkJSON{}
}

func healthy(t *testing.T) *testEnv {
	e := newEnv(t)
	f := withLaunchd(e)
	f.IsLoaded = true
	withRunner(e, tmDest)
	e.writeConfig(shippedConfig())
	e.mkdirs(dev + "/")
	e.installAgent(goodAgent())
	st := &state.State{LastScheduled: &state.Scheduled{At: fixedTime, FDA: "granted"}}
	state.Save(e.fs, state.PathsFor(home), st)
	return e
}

var configTemplate = config.Template

func shippedConfig() string { return configTemplate([]string{"~/Developer"}) }

func TestDoctorAllOK(t *testing.T) {
	e := healthy(t)
	checks := doctorChecks(t, e, 0)
	var names []string
	for _, c := range checks {
		names = append(names, c.Name)
		if c.Status == "fail" || c.Status == "warn" {
			t.Errorf("check %+v", c)
		}
	}
	want := []string{"config", "time machine", "full disk access", "agent", "agent binary", "agent config", "agent interval",
		"group node", "group python", "group python-venv", "group jvm-build", "group rust", "group swiftpm",
		"group nobackup-marker", "group dev-caches", "version"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("checks = %v", names)
	}
	e.mustRun(0, "doctor")
	if got := lastLine(e.stdout.String()); got != "→ all good — status: tmskip status" {
		t.Fatalf("hint %q", got)
	}
}

func TestDoctorNoTMDestination(t *testing.T) {
	e := healthy(t)
	withRunner(e, map[string]runner.Result{"/usr/bin/tmutil destinationinfo": {Stdout: "tmutil: No destinations configured.\n"}})
	checks := doctorChecks(t, e, 0)
	if c := findCheck(checks, "time machine"); c.Status != "warn" || c.Fix == "" {
		t.Fatalf("check = %+v", c)
	}
}

func TestDoctorFDAFromLastScheduledRun(t *testing.T) {
	e := healthy(t)
	e.writeConfig(configTemplate([]string{"~/Documents/code"}))
	e.mkdirs(home + "/Documents/code/")
	state.Save(e.fs, state.PathsFor(home), &state.State{LastScheduled: &state.Scheduled{At: fixedTime, FDA: "denied"}})
	checks := doctorChecks(t, e, 1)
	c := findCheck(checks, "full disk access")
	if c.Status != "fail" || !strings.Contains(c.Fix, "Full Disk Access") || !strings.Contains(c.Fix, "/opt/homebrew/bin/tmskip") {
		t.Fatalf("check = %+v", c)
	}
	if !strings.Contains(c.Detail, home+"/Documents/code") {
		t.Fatalf("detail should name the path that needs it: %+v", c)
	}
	e.mustRun(1, "doctor")
	if got := lastLine(e.stdout.String()); got != "→ "+c.Fix {
		t.Fatalf("hint %q, want first failing fix", got)
	}
}

func TestDoctorFDANotNeeded(t *testing.T) {
	e := healthy(t)
	state.Save(e.fs, state.PathsFor(home), &state.State{LastScheduled: &state.Scheduled{At: fixedTime, FDA: "denied"}})
	if c := findCheck(doctorChecks(t, e, 0), "full disk access"); c.Status != "info" {
		t.Fatalf("check = %+v", c)
	}
	state.Save(e.fs, state.PathsFor(home), &state.State{})
	if c := findCheck(doctorChecks(t, e, 0), "full disk access"); c.Status != "info" || !strings.Contains(c.Detail, "no scheduled run") {
		t.Fatalf("check = %+v", c)
	}
}

func TestDoctorAgentChecks(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(e *testEnv)
		check  string
	}{
		{"binary missing", func(e *testEnv) { e.fs.RemoveAll("/opt/homebrew/bin/tmskip") }, "agent binary"},
		{"binary differs", func(e *testEnv) {
			a := goodAgent()
			a.Binary = "/usr/local/bin/tmskip"
			e.fs.WriteFile(a.Binary, []byte("x"))
			e.installAgent(a)
		}, "agent binary"},
		{"config differs", func(e *testEnv) { a := goodAgent(); a.Config = home + "/old.toml"; e.installAgent(a) }, "agent config"},
		{"interval differs", func(e *testEnv) { a := goodAgent(); a.Interval = 2 * time.Hour; e.installAgent(a) }, "agent interval"},
		{"not loaded", func(e *testEnv) { e.deps = chainLaunchdUnloaded(e.deps) }, "agent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := healthy(t)
			c.mutate(e)
			got := findCheck(doctorChecks(t, e, 1), c.check)
			if got.Status != "fail" || !strings.Contains(got.Fix, "tmskip schedule on") {
				t.Fatalf("check = %+v", got)
			}
		})
	}
}

func chainLaunchdUnloaded(prev func(*Deps)) func(*Deps) {
	return func(d *Deps) {
		prev(d)
		d.Launchd.Bootout("x") // fake: marks it unloaded
	}
}

func TestDoctorScheduleOffIsInfo(t *testing.T) {
	e := healthy(t)
	e.fs.RemoveAll(plistPath)
	e.deps = chainLaunchdUnloaded(e.deps)
	checks := doctorChecks(t, e, 0)
	if c := findCheck(checks, "agent"); c.Status != "info" {
		t.Fatalf("check = %+v", c)
	}
	if c := findCheck(checks, "agent binary"); c.Name != "" {
		t.Fatalf("agent detail checks without an agent: %+v", c)
	}
}

func TestDoctorGroups(t *testing.T) {
	e := healthy(t)
	src := configTemplate([]string{"~/Developer"})
	src = strings.Replace(src, `names = ["node_modules", ".next"]`, `names = ["node_modules"]`, 1)
	i := strings.Index(src, "# rust")
	j := strings.Index(src, "# swiftpm")
	src = src[:i] + src[j:]
	e.writeConfig(src)
	checks := doctorChecks(t, e, 0)
	if c := findCheck(checks, "group node"); c.Status != "info" || c.Detail != "changed in your config" {
		t.Fatalf("node = %+v", c)
	}
	if c := findCheck(checks, "group rust"); c.Detail != "not in your config" || !strings.Contains(c.Fix, "tmskip config add rust") {
		t.Fatalf("rust = %+v", c)
	}
	if c := findCheck(checks, "group python"); c.Detail != "in your config" {
		t.Fatalf("python = %+v", c)
	}
}

func TestDoctorInvalidConfigExits2(t *testing.T) {
	e := healthy(t)
	e.writeConfig("version = 3\n")
	checks := doctorChecks(t, e, 2)
	if c := findCheck(checks, "config"); c.Status != "fail" || !strings.Contains(c.Detail, "unsupported version 3") {
		t.Fatalf("config = %+v", c)
	}
	if findCheck(checks, "version").Name == "" {
		t.Fatal("doctor stopped at the config check")
	}
}

func TestDoctorShowsVersion(t *testing.T) {
	e := healthy(t)
	if c := findCheck(doctorChecks(t, e, 0), "version"); c.Detail != "tmskip 1.2.3" {
		t.Fatalf("version = %+v", c)
	}
	e.mustRun(0, "doctor", "--json")
	golden(t, "doctor.golden.json", e.stdout.Bytes())
}

func TestNeedsFDA(t *testing.T) {
	e := newEnv(t)
	e.mkdirs(home+"/Documents/code/", dev+"/", "/Volumes/X/m/", home+"/Library/Mobile Documents/x/")
	e.writeConfig(`version = 1
roots = ["~/Developer", "~/Documents/code", "~/Library/Mobile Documents/x"]
[[rule]]
name = "m"
paths = ["/Volumes/X/m", "~/Developer/x"]
`)
	a := &app{d: e.Deps(), g: &globals{}}
	a.out = newOutput(a.d.Env, a.g)
	r, _, err := a.loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	got := needsFDA(r)
	want := []string{home + "/Documents/code", home + "/Library/Mobile Documents/x", "/Volumes/X/m"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("needsFDA = %v", got)
	}
	e.writeConfig("version = 1\nroots = [\"~\"]\n")
	r, _, _ = a.loadConfig()
	if got := needsFDA(r); len(got) != 1 || got[0] != home {
		t.Fatalf("home as a root contains protected folders: %v", got)
	}
}

func TestDoctorAgentBinaryDanglingSymlink(t *testing.T) {
	e := healthy(t)
	e.fs.RemoveAll("/opt/homebrew/bin/tmskip")
	e.fs.Symlink("../Cellar/tmskip/1.0/bin/tmskip", "/opt/homebrew/bin/tmskip") // keg removed
	c := findCheck(doctorChecks(t, e, 1), "agent binary")
	if c.Status != "fail" || !strings.Contains(c.Detail, "missing") {
		t.Fatalf("check = %+v", c)
	}
}

package cli

import (
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func interactive(e *testEnv, answers ...string) {
	e.tty = true
	e.vars["NO_COLOR"] = "1" // compare plain text
	e.stdin = strings.Join(answers, "\n") + "\n"
}

func TestInitRequiresTTY(t *testing.T) {
	e := newEnv(t)
	e.mustRun(1, "init")
	if !strings.Contains(e.stderr.String(), `use "tmskip config reset"`) {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	if _, err := e.fs.Lstat(cfgPath); err == nil {
		t.Fatal("wrote a config")
	}
}

func TestInitWritesConfigWithChosenRoots(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	withRunner(e, nil)
	e.mkdirs(home+"/Developer/", home+"/code/")
	interactive(e, "y", "n", "n", "n") // ~/Developer yes, ~/code no, apply no, schedule no
	e.mustRun(0, "init")
	got, err := e.fs.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `roots = ["~/Developer"]`) {
		t.Fatalf("config:\n%s", got)
	}
	out := e.stdout.String()
	for _, q := range []string{"Scan ~/Developer?", "Scan ~/code?", "Apply now?", "Turn on the schedule (every 1h)?"} {
		if !strings.Contains(out, q) {
			t.Errorf("no %q prompt:\n%s", q, out)
		}
	}
}

func TestInitExistingConfigSkipsWrite(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	interactive(e, "n", "n")
	e.mustRun(0, "init")
	if got, _ := e.fs.ReadFile(cfgPath); string(got) != nodeConfig {
		t.Fatal("config rewritten")
	}
	if !strings.Contains(e.stdout.String(), "keeping it") {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
}

func TestInitPreviewThenApply(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/")
	interactive(e, "y", "n")
	e.mustRun(0, "init")
	out := e.stdout.String()
	preview := strings.Index(out, "(dry run — nothing changed)")
	apply := strings.Index(out, "Apply now?")
	if preview < 0 || apply < preview {
		t.Fatalf("preview must come before the apply prompt:\n%s", out)
	}
	if !e.excluded(dev + "/app/node_modules") {
		t.Fatal("not applied")
	}
}

func TestInitAllStepsSkippable(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	r := withRunner(e, nil)
	e.mkdirs(dev+"/app/node_modules/", home+"/code/")
	interactive(e, "n", "n", "n", "n", "n", "n")
	e.mustRun(0, "init")
	if _, err := e.fs.Lstat(cfgPath); err != nil {
		t.Fatal("config not written")
	}
	if len(e.fs.Ops()) != 0 || len(f.Calls) != 0 || len(r.Calls) != 0 {
		t.Fatalf("ops %v launchd %v runner %v", e.fs.Ops(), f.Calls, r.Calls)
	}
}

func TestInitAsksFDAOnlyWhenNeeded(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	r := withRunner(e, nil)
	e.writeConfig(strings.Replace(nodeConfig, `"~/Developer"`, `"~/Documents/code"`, 1))
	e.mkdirs(home + "/Documents/code/")
	interactive(e, "n", "y", "n") // apply no, open settings yes, schedule no
	e.mustRun(0, "init")
	if !strings.Contains(e.stdout.String(), "Full Disk Access") || !strings.Contains(e.stdout.String(), "/opt/homebrew/bin/tmskip") {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
	want := [][]string{{"/usr/bin/open", fdaSettingsURL}}
	if !reflect.DeepEqual(r.Calls, want) {
		t.Fatalf("calls = %v", r.Calls)
	}

	e2 := newEnv(t)
	withLaunchd(e2)
	r2 := withRunner(e2, nil)
	e2.writeConfig(nodeConfig)
	e2.mkdirs(dev + "/")
	interactive(e2, "n", "n")
	e2.mustRun(0, "init")
	if strings.Contains(e2.stdout.String(), "Full Disk Access") || len(r2.Calls) != 0 {
		t.Fatalf("asked for FDA without need:\n%s", e2.stdout.String())
	}
}

func TestInitScheduleOn(t *testing.T) {
	e := newEnv(t)
	f := withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	interactive(e, "n", "y")
	e.mustRun(0, "init")
	if !reflect.DeepEqual(f.Calls, []string{"bootstrap " + plistPath}) {
		t.Fatalf("calls = %v", f.Calls)
	}
	if got := lastLine(e.stdout.String()); got != "→ check it: tmskip status" {
		t.Fatalf("hint %q", got)
	}
}

func TestInitTakesLock(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.lock.Held = true
	interactive(e, "y", "y", "y")
	e.mustRun(1, "init")
	if _, err := e.fs.Lstat(cfgPath); err == nil {
		t.Fatal("wrote a config while locked")
	}
}

func TestEveryCommandEndsWithHint(t *testing.T) {
	type row struct {
		args  []string
		setup func(e *testEnv)
		code  int
		hint  string
	}
	configured := func(e *testEnv) {
		e.writeConfig(nodeConfig)
		e.mkdirs(dev + "/app/node_modules/")
	}
	synced := func(e *testEnv) { configured(e); e.mustRun(0, "sync") }
	scheduledOn := func(e *testEnv) { configured(e); e.mustRun(0, "schedule", "on") }
	rows := []row{
		{nil, func(*testEnv) {}, 2, "get started: tmskip init"},
		{[]string{"sync"}, func(*testEnv) {}, 2, "fix the config, then: tmskip doctor"},
		{nil, configured, 0, "run hourly: tmskip schedule on"},
		{[]string{"sync", "-n"}, configured, 0, "apply: tmskip sync"},
		{[]string{"sync"}, configured, 0, "see what's managed: tmskip list"},
		{[]string{"status"}, configured, 0, "run hourly: tmskip schedule on"},
		{[]string{"status"}, scheduledOn, 0, "details: tmskip list --sizes"},
		{[]string{"list"}, configured, 0, "nothing managed yet — preview: tmskip sync -n"},
		{[]string{"list"}, synced, 0, "why one is excluded: tmskip why <path>"},
		{[]string{"why", dev + "/app/node_modules"}, configured, 0, "preview the next sync: tmskip sync -n"},
		{[]string{"doctor"}, scheduledOn, 0, "all good — status: tmskip status"},
		{[]string{"doctor"}, func(e *testEnv) { scheduledOn(e); e.fs.RemoveAll("/opt/homebrew/bin/tmskip") }, 1, `run "tmskip schedule on" to update`},
		{[]string{"schedule", "on"}, configured, 0, "check it later: tmskip status"},
		{[]string{"schedule", "off"}, scheduledOn, 0, "turn it on: tmskip schedule on"},
		{[]string{"schedule"}, configured, 0, "turn it on: tmskip schedule on"},
		{[]string{"schedule"}, scheduledOn, 0, "check it: tmskip doctor"},
		{[]string{"uninstall"}, synced, 0, "remove the binary: brew uninstall tmskip"},
		{[]string{"config", "defaults"}, configured, 0, "add a group: tmskip config add <group>"},
		{[]string{"config", "add", "rust"}, configured, 0, "preview: tmskip sync -n"},
		{[]string{"config", "reset", "--yes"}, configured, 0, "preview: tmskip sync -n"},
		{[]string{"config", "path"}, configured, 0, "check it: tmskip doctor"},
	}
	for _, r := range rows {
		name := strings.Join(r.args, " ")
		t.Run("tmskip "+name, func(t *testing.T) {
			for _, mode := range []string{"", "-q", "--json"} {
				e := newEnv(t)
				withLaunchd(e)
				withRunner(e, tmDest)
				r.setup(e)
				args := append([]string{}, r.args...)
				if mode != "" {
					args = append(args, mode)
				}
				if mode == "--json" && !jsonCommand(r.args) {
					continue
				}
				e.mustRun(r.code, args...)
				last := lastLine(e.stdout.String())
				switch mode {
				case "":
					if last != "→ "+r.hint {
						t.Fatalf("last line %q, want hint %q", last, r.hint)
					}
				default:
					if strings.Contains(e.stdout.String(), "→ ") {
						t.Fatalf("%s printed a hint:\n%s", mode, e.stdout.String())
					}
				}
			}
		})
	}
	t.Run("tmskip init", func(t *testing.T) {
		e := newEnv(t)
		withLaunchd(e)
		configured(e)
		interactive(e, "n", "n")
		e.mustRun(0, "init")
		if got := lastLine(e.stdout.String()); got != "→ check it: tmskip status" {
			t.Fatalf("hint %q", got)
		}
	})
}

func jsonCommand(args []string) bool {
	if len(args) == 0 {
		return true // bare = status
	}
	switch args[0] {
	case "status", "list", "why", "doctor", "sync":
		return true
	}
	return false
}

func TestInitApplyWithUnreadableExits3(t *testing.T) {
	e := newEnv(t)
	withLaunchd(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/app/node_modules/", dev+"/locked/")
	e.fs.Deny(dev+"/locked", syscall.EACCES)
	interactive(e, "y", "n")
	e.mustRun(3, "init")
	if !e.excluded(dev + "/app/node_modules") {
		t.Fatal("not applied")
	}
}

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestWhyReasons(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(`version = 1
roots = ["~/Developer"]
skip = [".git"]
keep = ["~/Developer/notes"]

[[rule]]
name = "node"
names = ["node_modules"]

[[rule]]
name = "jvm-build"
names = ["build"]
parent_has = ["build.gradle"]
`)
	e.mkdirs(dev+"/svc/build.gradle", dev+"/svc/build/", dev+"/svc/src/",
		dev+"/notes/web/node_modules/", dev+"/repo/.git/node_modules/",
		home+"/Documents/node_modules/", dev+"/a/node_modules/b/node_modules/", dev+"/u/node_modules/")
	e.fs.SetXattr(dev+"/u/node_modules", "com.apple.metadata:com_apple_backup_excludeItem", []byte("x"))

	cases := []struct {
		path  string
		want  map[string]any
		lines []string
	}{
		{dev + "/svc/build", map[string]any{"decision": "exclude", "reason": "matched", "rule": "jvm-build", "key": "parent_has", "gate": "build.gradle", "owner": "none", "excluded_now": false},
			[]string{`matches rule "jvm-build" (parent_has: build.gradle)`, "next sync: excludes it"}},
		{dev + "/notes/web/node_modules", map[string]any{"decision": "none", "reason": "blocked", "guard": "is kept by " + dev + "/notes", "rule": "node"},
			[]string{"is kept by " + dev + "/notes"}},
		{dev, map[string]any{"decision": "none", "reason": "blocked", "guard": "is a scan root"}, []string{"is a scan root"}},
		{dev + "/repo/.git/node_modules", map[string]any{"decision": "none", "reason": "skipped"}, []string{"inside a skipped folder"}},
		{home + "/Documents/node_modules", map[string]any{"decision": "none", "reason": "outside_roots"}, []string{"outside roots and fixed paths"}},
		{dev + "/svc/src", map[string]any{"decision": "none", "reason": "not_matched"}, []string{"no rule matches"}},
		{dev + "/a/node_modules/b/node_modules", map[string]any{"decision": "exclude", "covered_by": dev + "/a/node_modules"}, []string{"inside " + dev + "/a/node_modules"}},
		{dev + "/u/node_modules", map[string]any{"owner": "user", "excluded_now": true, "decision": "none"}, []string{"excluded by you"}},
	}
	for _, c := range cases {
		t.Run(strings.TrimPrefix(c.path, dev), func(t *testing.T) {
			e.mustRun(0, "why", c.path, "--json")
			var got map[string]any
			if err := json.Unmarshal(e.stdout.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			for k, v := range c.want {
				if got[k] != v {
					t.Errorf("%s = %v, want %v (%s)", k, got[k], v, e.stdout.String())
				}
			}
			e.mustRun(0, "why", c.path)
			for _, l := range c.lines {
				if !strings.Contains(e.stdout.String(), l) {
					t.Errorf("text lacks %q:\n%s", l, e.stdout.String())
				}
			}
			if got := lastLine(e.stdout.String()); got != "→ preview the next sync: tmskip sync -n" {
				t.Errorf("hint %q", got)
			}
		})
	}
	e.mustRun(0, "why", dev+"/svc/build", "--json")
	golden(t, "why.golden.json", e.stdout.Bytes())
}

func TestWhyRelativeAndWrongCase(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/app/node_modules/")
	e.mustRun(0, "why", "Developer/app/node_modules", "--json") // cwd is $HOME
	if !strings.Contains(e.stdout.String(), `"path": "`+dev+`/app/node_modules"`) {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
}

func TestWhyMissingPathExits1(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	e.mustRun(1, "why", dev+"/nope")
	if !strings.Contains(e.stderr.String(), dev+"/nope") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	e.mustRun(1, "why")
}

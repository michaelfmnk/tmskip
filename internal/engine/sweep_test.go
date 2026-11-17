package engine

import (
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
)

func TestSweepFindsMarkedViaWalkFixedAndHints(t *testing.T) {
	m := tree(
		D+"/a/node_modules/inner/node_modules/", // marked, plus a nested marked one only reachable via hints
		D+"/lost/node_modules/",                 // marked, not in hints (state lost)
		D+"/plain/",
		"/Users/t/.npm/",        // fixed path, marked
		"/Users/t/moved/build/", // moved outside the roots, in hints
		D+"/.git/marked/",       // inside a skipped folder: never walked
	)
	r := resolved(t, m, `version = 1
roots = ["~/Developer"]
skip = [".git"]
[[rule]]
name = "caches"
paths = ["~/.npm"]
`)
	for _, p := range []string{D + "/a/node_modules", D + "/a/node_modules/inner/node_modules", D + "/lost/node_modules", "/Users/t/.npm", "/Users/t/moved/build", D + "/.git/marked"} {
		own(m, p)
	}
	ex := exclusion.New(m)
	plan := Sweep(r, m, ex, hints(D+"/a/node_modules/inner/node_modules", "/Users/t/moved/build", D+"/gone"))
	want := []string{
		"- /Users/t/.npm",
		"- " + D + "/a/node_modules",
		"- " + D + "/a/node_modules/inner/node_modules",
		"- " + D + "/lost/node_modules",
		"- /Users/t/moved/build",
	}
	if got := ops(plan); !reflect.DeepEqual(got, want) {
		t.Fatalf("ops = %q", got)
	}
	for _, c := range m.ReadDirCalls() {
		// Hints are canonicalised (their ancestors listed); the walk itself
		// must not descend into a marked folder or a skipped one.
		if c == D+"/lost/node_modules" || c == D+"/.git" {
			t.Fatalf("descended into %s", c)
		}
	}
	if len(plan.Hints) != 0 || len(plan.Problems) != 0 {
		t.Fatalf("hints %v problems %v", plan.Hints, plan.Problems)
	}
}

func TestSweepLeavesUserExclusions(t *testing.T) {
	m := tree(D + "/u/node_modules/")
	r := resolved(t, m, nodeCfg)
	userExclude(m, D+"/u/node_modules")
	plan := Sweep(r, m, exclusion.New(m), hints(D+"/u/node_modules"))
	if len(plan.Actions) != 0 {
		t.Fatalf("ops = %q", ops(plan))
	}
}

func TestSweepNilConfigUsesHintsOnly(t *testing.T) {
	m := tree(D+"/a/node_modules/", D+"/b/node_modules/")
	own(m, D+"/a/node_modules")
	own(m, D+"/b/node_modules")
	plan := Sweep(nil, m, exclusion.New(m), hints(D+"/a/node_modules"))
	if got := ops(plan); !reflect.DeepEqual(got, []string{"- " + D + "/a/node_modules"}) {
		t.Fatalf("ops = %q", got)
	}
	for _, c := range m.ReadDirCalls() {
		if !strings.HasPrefix(D+"/a/node_modules", c) { // only the hint's ancestors (canonicalisation)
			t.Fatalf("walked without a config: %v", m.ReadDirCalls())
		}
	}
}

func TestSweepUnreadableAndUnmounted(t *testing.T) {
	m := tree(D+"/locked/x/", D+"/ok/node_modules/", "/Volumes/USB/x/")
	r := resolved(t, m, nodeCfg)
	own(m, D+"/ok/node_modules")
	m.Deny(D+"/locked", syscall.EPERM)
	m.Unmount("/Volumes/USB")
	plan := Sweep(r, m, exclusion.New(m), hints("/Volumes/USB/x/node_modules"))
	if got := ops(plan); !reflect.DeepEqual(got, []string{"! " + D + "/locked", "- " + D + "/ok/node_modules", "! /Volumes/USB/x/node_modules"}) {
		t.Fatalf("ops = %q", got)
	}
	if len(plan.Problems) != 1 || plan.Problems[0].Path != D+"/locked" || !plan.Problems[0].Denied {
		t.Fatalf("problems = %+v", plan.Problems)
	}
	if got := hintPaths(plan); !reflect.DeepEqual(got, []string{"/Volumes/USB/x/node_modules"}) {
		t.Fatalf("hints = %v", got)
	}
}

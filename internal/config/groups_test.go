package config

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestGroupsOrderAndContent(t *testing.T) {
	gs := Groups()
	var names []string
	for _, g := range gs {
		names = append(names, g.Name)
		if g.Rule.Name != g.Name {
			t.Errorf("group %q has rule name %q", g.Name, g.Rule.Name)
		}
	}
	want := []string{"node", "python", "python-venv", "jvm-build", "rust", "swiftpm", "nobackup-marker", "dev-caches"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v", names)
	}
	byName := map[string]Rule{}
	for _, g := range gs {
		byName[g.Name] = g.Rule
	}
	check := func(got, want []string, what string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %v, want %v", what, got, want)
		}
	}
	check(byName["node"].Names, []string{"node_modules", ".next"}, "node names")
	check(byName["python"].Names, []string{"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".tox", ".nox"}, "python names")
	check(byName["python-venv"].Contains, []string{"pyvenv.cfg"}, "python-venv contains")
	check(byName["jvm-build"].Names, []string{"build", "target"}, "jvm-build names")
	check(byName["jvm-build"].ParentHas, []string{"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "pom.xml"}, "jvm-build parent_has")
	check(byName["rust"].Names, []string{"target"}, "rust names")
	check(byName["rust"].ParentHas, []string{"Cargo.toml"}, "rust parent_has")
	check(byName["swiftpm"].Names, []string{".build"}, "swiftpm names")
	check(byName["swiftpm"].ParentHas, []string{"Package.swift"}, "swiftpm parent_has")
	check(byName["nobackup-marker"].Contains, []string{".nobackup"}, "nobackup contains")
	check(byName["dev-caches"].Paths, []string{"~/.npm", "~/.m2/repository", "~/.gradle/caches", "~/.gradle/wrapper",
		"~/.cache/huggingface", "~/Library/Developer/Xcode/DerivedData", "~/Library/Developer/Xcode/iOS DeviceSupport",
		"~/Library/Developer/CoreSimulator"}, "dev-caches paths")
	for _, bad := range []string{"bin", "out"} {
		if slices.Contains(byName["jvm-build"].Names, bad) {
			t.Errorf("jvm-build must not include %q", bad)
		}
	}
}

func TestGroupTextIsVerbatimBlock(t *testing.T) {
	for _, g := range Groups() {
		if !strings.HasPrefix(g.Text, "#") || !strings.HasSuffix(g.Text, "\n") || strings.HasSuffix(g.Text, "\n\n") {
			t.Errorf("%s: text should start with its comment and end with one newline: %q", g.Name, g.Text)
		}
		if strings.Count(g.Text, "[[rule]]") != 1 {
			t.Errorf("%s: text holds %d rules", g.Name, strings.Count(g.Text, "[[rule]]"))
		}
		f, err := Parse([]byte("version = 1\n" + g.Text))
		if err != nil {
			t.Fatalf("%s: %v", g.Name, err)
		}
		if err := f.Validate(); err != nil {
			t.Fatalf("%s: %v", g.Name, err)
		}
		if len(f.Rules) != 1 || !reflect.DeepEqual(f.Rules[0], g.Rule) {
			t.Errorf("%s: parsed %+v, want %+v", g.Name, f.Rules, g.Rule)
		}
	}
}

func TestTemplateParsesAndValidates(t *testing.T) {
	tpl := Template([]string{"~/Developer", "~/My \"Projects\""})
	f, err := Parse([]byte(tpl))
	if err != nil {
		t.Fatalf("%v\n%s", err, tpl)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if !f.Notify || f.Schedule == nil || f.Schedule.Interval != "1h" {
		t.Fatalf("notify/schedule: %+v", f)
	}
	if !reflect.DeepEqual(f.Skip, []string{".git"}) || len(f.Keep) != 0 || f.Keep == nil {
		t.Fatalf("skip/keep: %v %v", f.Skip, f.Keep)
	}
	if !reflect.DeepEqual(f.Roots, []string{"~/Developer", "~/My \"Projects\""}) {
		t.Fatalf("roots = %v", f.Roots)
	}
	if len(f.Rules) != len(Groups()) {
		t.Fatalf("rules = %d", len(f.Rules))
	}
	for _, g := range Groups() {
		if !strings.Contains(tpl, g.Text) {
			t.Errorf("template lacks verbatim %s block", g.Name)
		}
	}
}

func TestTemplateEmptyRoots(t *testing.T) {
	tpl := Template(nil)
	f, err := Parse([]byte(tpl))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if f.Roots == nil || len(f.Roots) != 0 {
		t.Fatalf("roots = %#v", f.Roots)
	}
	found := false
	for _, line := range strings.Split(tpl, "\n") {
		if strings.HasPrefix(line, "roots = []") && strings.Contains(line, "~/Developer") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no roots line suggesting ~/Developer:\n%s", tpl)
	}
}

func TestGroupStatus(t *testing.T) {
	node := Groups()[0]
	f := &File{Rules: []Rule{{Name: "node", Names: []string{".next", "node_modules"}}}}
	if s := GroupStatus(node, f); s != "present" {
		t.Fatalf("same set, other order → %s", s)
	}
	f.Rules[0].Names = append(f.Rules[0].Names, "bower_components")
	if s := GroupStatus(node, f); s != "changed" {
		t.Fatalf("extra name → %s", s)
	}
	if s := GroupStatus(node, &File{}); s != "missing" {
		t.Fatalf("absent → %s", s)
	}
}

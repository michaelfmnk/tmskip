package config

import (
	"errors"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

func compile(t *testing.T, m *memfs.FS, src string) (*Resolved, []Warning) {
	t.Helper()
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	r, ws, err := Compile(f, "/Users/t/.config/tmskip/config.toml", envWith(nil), m)
	if err != nil {
		t.Fatal(err)
	}
	return r, ws
}

func warningTexts(ws []Warning) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Text)
	}
	return out
}

func TestCompileExpandsTildeAndCanonicalises(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t/Developer")
	m.Mkdirs("/Users/t/.npm")
	r, ws := compile(t, m, `version = 1
roots = ["~/Developer", "/Users/t/./Developer/../Developer"]
skip = [".git"]
keep = ["~/Developer"]
notify = true
[schedule]
interval = "2h"
[[rule]]
name = "node"
names = ["node_modules"]
[[rule]]
name = "caches"
paths = ["~/.npm"]
`)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	if r.Home != "/Users/t" || !reflect.DeepEqual(r.Roots, []string{"/Users/t/Developer"}) ||
		!reflect.DeepEqual(r.Keep, []string{"/Users/t/Developer"}) || !r.Skip[".git"] || !r.Notify ||
		r.Interval != 2*time.Hour || r.Path != "/Users/t/.config/tmskip/config.toml" {
		t.Fatalf("resolved = %+v", r)
	}
	if len(r.Rules) != 2 || !r.Rules[0].Names["node_modules"] || !reflect.DeepEqual(r.Rules[1].Fixed, []string{"/Users/t/.npm"}) {
		t.Fatalf("rules = %+v", r.Rules)
	}
	if rule, ok := r.IsFixed("/Users/t/.npm"); !ok || rule != "caches" {
		t.Fatalf("IsFixed = %q %v", rule, ok)
	}
	if _, ok := r.IsFixed("/Users/t/Developer"); ok {
		t.Fatal("root reported fixed")
	}
}

func TestCompileWrongCaseConfigPaths(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	m.Mkdirs("/Users/t/Developer/notes")
	r, ws := compile(t, m, `version = 1
roots = ["~/developer"]
keep = ["~/developer/notes"]
`)
	if len(ws) != 0 {
		t.Fatalf("warnings: %v", ws)
	}
	if !reflect.DeepEqual(r.Roots, []string{"/Users/t/Developer"}) || !reflect.DeepEqual(r.Keep, []string{"/Users/t/Developer/notes"}) {
		t.Fatalf("roots %v keep %v", r.Roots, r.Keep)
	}
}

func TestCompileDedupesOverlappingRoots(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t/Developer/work")
	r, _ := compile(t, m, `version = 1
roots = ["~/Developer/work", "~/Developer"]
`)
	if !reflect.DeepEqual(r.Roots, []string{"/Users/t/Developer"}) {
		t.Fatalf("roots = %v", r.Roots)
	}
	if !reflect.DeepEqual(r.Notes, []string{"root ~/Developer/work is inside ~/Developer — ignored"}) {
		t.Fatalf("notes = %v", r.Notes)
	}
}

func TestCompileDedupesAliasedRoots(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t/Developer")
	m.Symlink("/Users/t/Developer", "/Users/t/code")
	r, _ := compile(t, m, `version = 1
roots = ["~/Developer", "~/code"]
`)
	if !reflect.DeepEqual(r.Roots, []string{"/Users/t/Developer"}) {
		t.Fatalf("roots = %v", r.Roots)
	}
	if len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "~/code") || !strings.Contains(r.Notes[0], "ignored") {
		t.Fatalf("notes = %v", r.Notes)
	}
}

func TestCompileHomeThroughSymlink(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Volumes/Data/t/Developer")
	m.Symlink("/Volumes/Data/t", "/Users/t")
	r, _ := compile(t, m, `version = 1
roots = ["~/Developer"]
`)
	if r.Home != "/Volumes/Data/t" || !reflect.DeepEqual(r.Roots, []string{"/Volumes/Data/t/Developer"}) {
		t.Fatalf("home %q roots %v", r.Home, r.Roots)
	}
}

func TestCompileWarnsMissingRootsAndKeep(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t")
	r, ws := compile(t, m, `version = 1
roots = ["~/Develper"]
keep = ["~/Developer/ntoes"]
`)
	want := []string{"root ~/Develper does not exist", "keep ~/Developer/ntoes does not exist"}
	if !reflect.DeepEqual(warningTexts(ws), want) {
		t.Fatalf("warnings = %v", warningTexts(ws))
	}
	// Missing paths are still kept (canonical prefix + typed tail): a keep
	// protects as soon as the folder appears; a missing root is skipped by the walk.
	if !reflect.DeepEqual(r.Roots, []string{"/Users/t/Develper"}) || !reflect.DeepEqual(r.Keep, []string{"/Users/t/Developer/ntoes"}) {
		t.Fatalf("roots %v keep %v", r.Roots, r.Keep)
	}
}

func TestCompileDropsDisabledRulesAndMissingFixed(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t/.npm")
	r, ws := compile(t, m, `version = 1
[[rule]]
name = "off"
names = ["x"]
enabled = false
[[rule]]
name = "caches"
paths = ["~/.npm", "~/.m2/repository"]
`)
	if len(ws) != 0 {
		t.Fatalf("missing fixed paths must be silent: %v", ws)
	}
	if len(r.Rules) != 1 || r.Rules[0].Name != "caches" || !reflect.DeepEqual(r.Rules[0].Fixed, []string{"/Users/t/.npm"}) {
		t.Fatalf("rules = %+v", r.Rules)
	}
	if len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "~/.m2/repository") {
		t.Fatalf("notes = %v", r.Notes)
	}
}

func TestCompileFixedSymlinkActsOnTarget(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Volumes/X/models")
	m.Symlink("/Volumes/X/models", "/Users/t/.lmstudio/models")
	r, _ := compile(t, m, `version = 1
[[rule]]
name = "my-models"
paths = ["~/.lmstudio/models"]
`)
	if !reflect.DeepEqual(r.Rules[0].Fixed, []string{"/Volumes/X/models"}) {
		t.Fatalf("fixed = %v", r.Rules[0].Fixed)
	}
}

func TestCompileNoHome(t *testing.T) {
	f, _ := Parse([]byte("version = 1\n"))
	env := envWith(nil)
	env.Home = ""
	if _, _, err := Compile(f, "/c.toml", env, memfs.New()); err == nil || !strings.Contains(err.Error(), "HOME is not set") {
		t.Fatalf("err = %v", err)
	}
}

// Review R3: a configured path under a folder that can't be searched (no x
// bit, or TCC) is a read problem, never an invalid config.
func TestCompileUnreadableComponentIsReadProblem(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t/Developer")
	m.Mkdirs("/Users/t/Private/x")
	m.Mkdirs("/Users/t/Private/code")
	m.Mkdirs("/Users/t/Private/cache")
	m.DenySearch("/Users/t/Private", syscall.EACCES)
	f, _ := Parse([]byte(`version = 1
roots = ["~/Developer", "~/Private/code"]
keep = ["~/Private/x"]
[[rule]]
name = "c"
paths = ["~/Private/cache"]
`))
	r, ws, err := Compile(f, "/c.toml", envWith(nil), m)
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	var texts []string
	for _, w := range ws {
		texts = append(texts, w.Text)
	}
	want := []string{
		"root ~/Private/code can't be read: permission denied",
		"keep ~/Private/x can't be read: permission denied",
		"fixed path ~/Private/cache (c) can't be read: permission denied",
	}
	if !reflect.DeepEqual(texts, want) {
		t.Fatalf("warnings = %q", texts)
	}
	if !reflect.DeepEqual(r.Keep, []string{"/Users/t/Private/x"}) || !reflect.DeepEqual(r.Roots, []string{"/Users/t/Developer", "/Users/t/Private/code"}) {
		t.Fatalf("keep %v roots %v", r.Keep, r.Roots)
	}
	if len(r.Unreadable) != 3 || r.Unreadable[1].Path != "/Users/t/Private/x" || !errors.Is(r.Unreadable[1].Err, syscall.EACCES) {
		t.Fatalf("unreadable = %+v", r.Unreadable)
	}
}

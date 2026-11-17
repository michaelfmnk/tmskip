package engine

import (
	"reflect"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// Review round 2, N1: when a root, keep or fixed path can't be resolved
// (a component can't be searched: mode 000, or TCC without Full Disk
// Access), the config can't be applied safely — a symlink behind the
// unreadable folder may point anywhere. The run changes nothing: no +, no
// -, no marker changes; one problem per unreadable path; hints kept.
func assertNothingChanges(t *testing.T, m *memfs.FS, cfg string, hs []state.Owned, watch ...string) {
	t.Helper()
	r := resolved(t, m, cfg)
	if len(r.Unreadable) == 0 {
		t.Fatal("fixture: nothing unreadable")
	}
	before := map[string]map[string][]byte{}
	for _, p := range watch {
		before[p] = m.Xattrs(p)
	}
	ex := exclusion.New(m)
	m.ResetOps()
	plan := Reconcile(r, m, ex, Scan(r, m, ex), hs)
	if len(plan.Actions) != 0 {
		t.Fatalf("plan has actions: %q", ops(plan))
	}
	if len(plan.Problems) != len(r.Unreadable) {
		t.Fatalf("problems = %+v", plan.Problems)
	}
	for _, p := range plan.Problems {
		if !p.Denied {
			t.Fatalf("EPERM problem not marked denied: %+v", p)
		}
	}
	if got := hintPaths(plan); !reflect.DeepEqual(got, func() []string {
		var out []string
		for _, h := range hs {
			out = append(out, h.Path)
		}
		return out
	}()) {
		t.Fatalf("hints = %v", got)
	}
	Apply(r, m, ex, plan)
	if len(m.Ops()) != 0 {
		t.Fatalf("xattr ops: %v", m.Ops())
	}
	for _, p := range watch {
		if !reflect.DeepEqual(m.Xattrs(p), before[p]) {
			t.Fatalf("%s xattrs changed: %v → %v", p, before[p], m.Xattrs(p))
		}
	}
}

// (a) root spelled through a symlink inside an unreadable folder: the owned
// folder under the real root must not be re-included.
func TestUnresolvableRootChangesNothing(t *testing.T) {
	m := memfs.New()
	nm := "/Users/t/Developer/app/node_modules"
	m.Mkdirs(nm)
	m.Mkdirs("/Users/t/Developer/web/node_modules") // new match: must not be excluded either
	m.Symlink("/Users/t/Developer", "/Users/t/Locked/dev")
	own(m, nm)
	m.DenySearch("/Users/t/Locked", syscall.EPERM)
	assertNothingChanges(t, m, `version = 1
roots = ["~/Locked/dev"]
[[rule]]
name = "node"
names = ["node_modules"]
`, hints(nm), nm, "/Users/t/Developer/web/node_modules")
}

// (b) keep spelled through such a symlink: the kept folder must not be excluded.
func TestUnresolvableKeepChangesNothing(t *testing.T) {
	m := memfs.New()
	secret := "/Users/t/Developer/app/secret/node_modules"
	m.Mkdirs(secret)
	m.Symlink("/Users/t/Developer/app", "/Users/t/Locked/app")
	m.DenySearch("/Users/t/Locked", syscall.EPERM)
	assertNothingChanges(t, m, `version = 1
roots = ["~/Developer"]
keep = ["~/Locked/app/secret"]
[[rule]]
name = "node"
names = ["node_modules"]
`, nil, secret)
}

// (c) fixed path through such a symlink: its owned target stays excluded.
func TestUnresolvableFixedChangesNothing(t *testing.T) {
	m := memfs.New()
	models := "/Users/t/LLM/models"
	m.Mkdirs(models)
	m.Mkdirs("/Users/t/Developer")
	m.Symlink(models, "/Users/t/Locked/models")
	own(m, models)
	m.DenySearch("/Users/t/Locked", syscall.EPERM)
	assertNothingChanges(t, m, `version = 1
roots = ["~/Developer"]
[[rule]]
name = "my-models"
paths = ["~/Locked/models"]
`, hints(models), models)
}

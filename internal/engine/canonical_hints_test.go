package engine

import (
	"reflect"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// Review R2 (a): the root was renamed in case only on a case-insensitive
// volume; the stored hint keeps the old case. It must not be re-included.
func TestReconcileStaleCaseHint(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	nm := "/Users/t/Developer/app/node_modules"
	m.Mkdirs(nm)
	own(m, nm)
	r := resolved(t, m, nodeCfg)
	ex := exclusion.New(m)
	size := int64(42)
	plan := Reconcile(r, m, ex, Scan(r, m, ex), []state.Owned{{Path: "/Users/t/developer/app/node_modules", Bytes: &size}})
	if got := ops(plan); !reflect.DeepEqual(got, []string{"= " + nm}) {
		t.Fatalf("ops = %q", got)
	}
	if len(plan.Hints) != 1 || plan.Hints[0].Path != nm || plan.Hints[0].Bytes == nil || *plan.Hints[0].Bytes != 42 {
		t.Fatalf("hints = %+v", plan.Hints)
	}
	Apply(r, m, ex, plan)
	if ok, _ := ex.IsExcluded(nm); !ok {
		t.Fatal("re-included")
	}
}

// Review R2 (b): the project moved under a keep path and was symlinked back
// into the root. The hint goes through the symlink; it must never be
// excluded (it is under keep).
func TestReconcileHintThroughSymlinkIntoKeep(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Users/t/Developer")
	real := "/Users/t/Archive/app/node_modules"
	m.Mkdirs(real)
	m.Symlink("/Users/t/Archive/app", "/Users/t/Developer/app")
	markOnly(m, real) // marked, exclusion removed
	r := resolved(t, m, `version = 1
roots = ["~/Developer"]
keep = ["~/Archive"]
[[rule]]
name = "node"
names = ["node_modules"]
`)
	ex := exclusion.New(m)
	plan := Reconcile(r, m, ex, Scan(r, m, ex), hints("/Users/t/Developer/app/node_modules"))
	if got := ops(plan); !reflect.DeepEqual(got, []string{"- " + real}) {
		t.Fatalf("ops = %q", got)
	}
	Apply(r, m, ex, plan)
	if ok, _ := ex.IsExcluded(real); ok {
		t.Fatal("excluded a folder under keep through a symlink")
	}
	if ok, _ := ex.IsMarked(real); ok {
		t.Fatal("still marked")
	}
}

// Review R2: Apply refuses to exclude a path that isn't canonical.
func TestApplyRefusesNonCanonicalPath(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	m.Mkdirs("/Users/t/Developer/app/node_modules")
	m.Mkdirs("/Users/t/Archive/x/node_modules")
	m.Symlink("/Users/t/Archive/x", "/Users/t/Developer/x")
	r := resolved(t, m, nodeCfg)
	plan := &Plan{Actions: []Action{
		{Op: OpExclude, Path: "/Users/t/developer/app/node_modules"},
		{Op: OpExclude, Path: "/Users/t/Developer/x/node_modules"},
	}}
	_, _, problems := Apply(r, m, exclusion.New(m), plan)
	if len(problems) != 2 || len(m.Ops()) != 0 {
		t.Fatalf("problems %+v ops %v", problems, m.Ops())
	}
}

func TestSweepCanonicalisesHints(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	nm := "/Users/t/Developer/app/node_modules"
	m.Mkdirs(nm)
	own(m, nm)
	plan := Sweep(nil, m, exclusion.New(m), hints("/Users/t/developer/app/node_modules", nm))
	if got := ops(plan); !reflect.DeepEqual(got, []string{"- " + nm}) {
		t.Fatalf("ops = %q", got)
	}
}

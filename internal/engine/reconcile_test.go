package engine

import (
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// own gives p tmskip's marker and an exclusion.
func own(m *memfs.FS, p string) {
	m.SetXattr(p, exclusion.MarkerName, exclusion.MarkerValue)
	m.SetXattr(p, exclusion.AttrName, exclusion.Value)
}

func markOnly(m *memfs.FS, p string) { m.SetXattr(p, exclusion.MarkerName, exclusion.MarkerValue) }

func userExclude(m *memfs.FS, p string) { m.SetXattr(p, exclusion.AttrName, exclusion.Value) }

func hints(paths ...string) []state.Owned {
	var out []state.Owned
	for _, p := range paths {
		out = append(out, state.Owned{Path: p})
	}
	return out
}

func ops(p *Plan) []string {
	var out []string
	for _, a := range p.Actions {
		out = append(out, a.Op.Symbol()+" "+a.Path)
	}
	return out
}

func hintPaths(p *Plan) []string {
	var out []string
	for _, h := range p.Hints {
		out = append(out, h.Path)
	}
	return out
}

const nodeCfg = `version = 1
roots = ["~/Developer"]
[[rule]]
name = "node"
names = ["node_modules"]
[[rule]]
name = "nobackup"
contains = [".nobackup"]
`

func TestReconcile(t *testing.T) {
	nm := D + "/app/node_modules"
	cases := []struct {
		name      string
		files     []string
		setup     func(m *memfs.FS)
		cfg       string
		hints     []state.Owned
		wantOps   []string
		wantHints []string
		problems  int
	}{
		{"new match", []string{nm + "/"}, nil, nodeCfg, nil, []string{"+ " + nm}, []string{nm}, 0},
		{"owned and excluded", []string{nm + "/"}, func(m *memfs.FS) { own(m, nm) }, nodeCfg, hints(nm), []string{"= " + nm}, []string{nm}, 0},
		{"marked not excluded, matches → repair", []string{nm + "/"}, func(m *memfs.FS) { markOnly(m, nm) }, nodeCfg, hints(nm), []string{"+ " + nm}, []string{nm}, 0},
		{"user exclusion never touched", []string{nm + "/"}, func(m *memfs.FS) { userExclude(m, nm) }, nodeCfg, nil, []string{"· " + nm}, nil, 0},
		{"owned no longer matches", []string{D + "/app/vendor/"}, func(m *memfs.FS) { own(m, D+"/app/vendor") }, nodeCfg, hints(D + "/app/vendor"), []string{"- " + D + "/app/vendor"}, nil, 0},
		{"marked not excluded, no match", []string{D + "/app/vendor/"}, func(m *memfs.FS) { markOnly(m, D+"/app/vendor") }, nodeCfg, hints(D + "/app/vendor"), []string{"- " + D + "/app/vendor"}, nil, 0},
		{"hint outside every root (root narrowed)", []string{"/Users/t/old/app/node_modules/", D + "/"}, func(m *memfs.FS) { own(m, "/Users/t/old/app/node_modules") }, nodeCfg,
			hints("/Users/t/old/app/node_modules"), []string{"- /Users/t/old/app/node_modules"}, nil, 0},
		{"hint now under keep", []string{nm + "/"}, func(m *memfs.FS) { own(m, nm) }, strings.Replace(nodeCfg, "\n[[rule]]", "\nkeep = [\"~/Developer/app\"]\n[[rule]]", 1),
			hints(nm), []string{"- " + nm}, nil, 0},
		{"rule disabled", []string{nm + "/"}, func(m *memfs.FS) { own(m, nm) }, strings.Replace(nodeCfg, `names = ["node_modules"]`, "names = [\"node_modules\"]\nenabled = false", 1),
			hints(nm), []string{"- " + nm}, nil, 0},
		{".nobackup removed", []string{D + "/data/x"}, func(m *memfs.FS) { own(m, D+"/data") }, nodeCfg, hints(D + "/data"), []string{"- " + D + "/data"}, nil, 0},
		{".nobackup present", []string{D + "/data/.nobackup"}, func(m *memfs.FS) { own(m, D+"/data") }, nodeCfg, hints(D + "/data"), []string{"= " + D + "/data"}, []string{D + "/data"}, 0},
		{"hint missing on boot volume → dropped", []string{D + "/"}, nil, nodeCfg, hints(D + "/gone/node_modules"), nil, nil, 0},
		{"hint missing on unmounted volume → kept", []string{D + "/", "/Volumes/USB/"}, func(m *memfs.FS) { m.Unmount("/Volumes/USB") }, nodeCfg,
			hints("/Volumes/USB/Developer/x/node_modules"), []string{"! /Volumes/USB/Developer/x/node_modules"}, []string{"/Volumes/USB/Developer/x/node_modules"}, 0},
		{"hint unreadable → untouched, kept, problem", []string{D + "/locked/node_modules/"}, func(m *memfs.FS) {
			own(m, D+"/locked/node_modules")
			m.Deny(D+"/locked", syscall.EPERM)
		}, nodeCfg, hints(D + "/locked/node_modules"), []string{"! " + D + "/locked/node_modules"}, []string{D + "/locked/node_modules"}, 2},
		{"hint without marker → dropped, untouched", []string{D + "/app/vendor/"}, func(m *memfs.FS) { userExclude(m, D+"/app/vendor") }, nodeCfg,
			hints(D + "/app/vendor"), nil, nil, 0},
		{"nested marked folder inside newly matched folder", []string{nm + "/x/node_modules/"}, func(m *memfs.FS) { own(m, nm+"/x/node_modules") }, nodeCfg,
			hints(nm + "/x/node_modules"), []string{"+ " + nm, "= " + nm + "/x/node_modules"}, []string{nm, nm + "/x/node_modules"}, 0},
		{"state lost, markers in tree", []string{nm + "/", D + "/app/vendor/"}, func(m *memfs.FS) {
			own(m, nm)
			own(m, D+"/app/vendor")
		}, nodeCfg, nil, []string{"= " + nm, "- " + D + "/app/vendor"}, []string{nm}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := tree(c.files...)
			r := resolved(t, m, c.cfg)
			if c.setup != nil {
				c.setup(m)
			}
			ex := exclusion.New(m)
			plan := Reconcile(r, m, ex, Scan(r, m, ex), c.hints)
			if got := ops(plan); !reflect.DeepEqual(got, c.wantOps) {
				t.Fatalf("ops = %q, want %q", got, c.wantOps)
			}
			if got := hintPaths(plan); !reflect.DeepEqual(got, c.wantHints) {
				t.Fatalf("hints = %q, want %q", got, c.wantHints)
			}
			if len(plan.Problems) != c.problems {
				t.Fatalf("problems = %+v", plan.Problems)
			}
			if len(m.Ops()) != 0 && c.setup == nil {
				t.Fatalf("reconcile wrote xattrs: %v", m.Ops())
			}
		})
	}
}

func TestReconcileCarriesBytesAndNotes(t *testing.T) {
	nm := D + "/app/node_modules"
	m := tree(nm+"/", D+"/app/vendor/")
	r := resolved(t, m, nodeCfg)
	own(m, nm)
	own(m, D+"/app/vendor")
	size := int64(4096)
	ex := exclusion.New(m)
	plan := Reconcile(r, m, ex, Scan(r, m, ex), []state.Owned{{Path: nm, Bytes: &size}})
	if len(plan.Hints) != 1 || plan.Hints[0].Bytes == nil || *plan.Hints[0].Bytes != 4096 {
		t.Fatalf("hints = %+v", plan.Hints)
	}
	if plan.Actions[0].Rule != "node" || plan.Actions[0].Bytes != 4096 {
		t.Fatalf("keep action = %+v", plan.Actions[0])
	}
	if plan.Actions[1].Op != OpInclude || plan.Actions[1].Note != "no rule matches" {
		t.Fatalf("include action = %+v", plan.Actions[1])
	}
	if OpUserOwned.String() != "user_owned" || OpExclude.String() != "exclude" || OpKeep.Symbol() != "=" {
		t.Fatal("op names")
	}
}

func TestReconcileVanishedFolderIsSilent(t *testing.T) {
	nm := D + "/app/node_modules"
	m := tree(nm+"/", D+"/old/vendor/")
	r := resolved(t, m, nodeCfg)
	own(m, D+"/old/vendor")
	ex := exclusion.New(m)
	scan := Scan(r, m, ex)
	// Both disappear after the walk.
	m.FailXattr(nm, exclusion.MarkerName, syscall.ENOENT)
	m.FailXattr(D+"/old/vendor", exclusion.MarkerName, syscall.ENOENT)
	plan := Reconcile(r, m, ex, scan, hints(D+"/old/vendor"))
	if len(plan.Actions) != 0 || len(plan.Problems) != 0 || len(plan.Hints) != 0 {
		t.Fatalf("ops %q problems %+v hints %v", ops(plan), plan.Problems, plan.Hints)
	}
}

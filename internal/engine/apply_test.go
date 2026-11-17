package engine

import (
	"reflect"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func TestApplyMarksBeforeExcluding(t *testing.T) {
	p := D + "/app/node_modules"
	m := tree(p + "/")
	r := resolved(t, m, nodeCfg)
	plan := &Plan{Actions: []Action{{Op: OpExclude, Path: p, Rule: "node"}}, Hints: hints(p)}
	applied, hs, problems := Apply(r, m, exclusion.New(m), plan)
	want := []string{"set " + exclusion.MarkerName + " " + p, "set " + exclusion.AttrName + " " + p}
	if !reflect.DeepEqual(m.Ops(), want) {
		t.Fatalf("ops = %v", m.Ops())
	}
	if len(applied) != 1 || len(problems) != 0 || len(hs) != 1 || hs[0].Path != p {
		t.Fatalf("applied %v hints %v problems %v", applied, hs, problems)
	}
}

func TestApplyIncludesBeforeUnmarking(t *testing.T) {
	p := D + "/app/vendor"
	m := tree(p + "/")
	r := resolved(t, m, nodeCfg)
	own(m, p)
	m.ResetOps()
	plan := &Plan{Actions: []Action{{Op: OpInclude, Path: p}}}
	_, hs, problems := Apply(r, m, exclusion.New(m), plan)
	want := []string{"rm " + exclusion.AttrName + " " + p, "rm " + exclusion.MarkerName + " " + p}
	if !reflect.DeepEqual(m.Ops(), want) || len(hs) != 0 || len(problems) != 0 {
		t.Fatalf("ops = %v hints %v problems %v", m.Ops(), hs, problems)
	}
}

func TestApplyExcludeFailureKeepsMarkerAndHint(t *testing.T) {
	p := D + "/app/node_modules"
	m := tree(p + "/")
	r := resolved(t, m, nodeCfg)
	m.FailXattr(p, exclusion.AttrName, syscall.EPERM)
	plan := &Plan{Actions: []Action{{Op: OpExclude, Path: p, Rule: "node"}}, Hints: hints(p)}
	applied, hs, problems := Apply(r, m, exclusion.New(m), plan)
	if len(problems) != 1 || problems[0].Path != p || !problems[0].Denied {
		t.Fatalf("problems = %+v", problems)
	}
	if _, ok := m.Xattrs(p)[exclusion.MarkerName]; !ok {
		t.Fatal("marker removed")
	}
	if len(hs) != 1 || hs[0].Path != p || len(applied) != 0 {
		t.Fatalf("hints %v applied %v", hs, applied)
	}
}

func TestApplyIncludeFailureKeepsHint(t *testing.T) {
	p := D + "/app/vendor"
	m := tree(p + "/")
	r := resolved(t, m, nodeCfg)
	own(m, p)
	m.FailXattr(p, exclusion.AttrName, syscall.EACCES)
	_, hs, problems := Apply(r, m, exclusion.New(m), &Plan{Actions: []Action{{Op: OpInclude, Path: p}}})
	if len(problems) != 1 || len(hs) != 1 || hs[0].Path != p {
		t.Fatalf("hints %v problems %v", hs, problems)
	}
	if _, ok := m.Xattrs(p)[exclusion.MarkerName]; !ok {
		t.Fatal("unmarked although the exclusion is still there")
	}
}

func TestApplyRefusesBlockedPath(t *testing.T) {
	m := tree(D + "/")
	r := resolved(t, m, nodeCfg)
	plan := &Plan{Actions: []Action{{Op: OpExclude, Path: "/Users/t"}, {Op: OpExclude, Path: D}}, Hints: hints("/Users/t", D)}
	applied, hs, problems := Apply(r, m, exclusion.New(m), plan)
	if len(m.Ops()) != 0 {
		t.Fatalf("xattr ops on a blocked path: %v", m.Ops())
	}
	if len(problems) != 2 || len(applied) != 0 || len(hs) != 0 {
		t.Fatalf("applied %v hints %v problems %+v", applied, hs, problems)
	}
}

func TestApplyContinuesAfterError(t *testing.T) {
	a, b := D+"/a/node_modules", D+"/b/node_modules"
	m := tree(a+"/", b+"/")
	r := resolved(t, m, nodeCfg)
	m.FailXattr(a, exclusion.MarkerName, syscall.EIO)
	plan := &Plan{Actions: []Action{{Op: OpExclude, Path: a}, {Op: OpExclude, Path: b}}, Hints: hints(a, b)}
	applied, hs, problems := Apply(r, m, exclusion.New(m), plan)
	if len(problems) != 1 || len(applied) != 1 || applied[0].Path != b {
		t.Fatalf("applied %v problems %v", applied, problems)
	}
	if !reflect.DeepEqual(hs, []state.Owned{{Path: b}}) {
		t.Fatalf("hints = %v", hs)
	}
	if _, ok := m.Xattrs(a)[exclusion.AttrName]; ok {
		t.Fatal("excluded without a marker")
	}
}

func TestApplyNilConfigSkipsGuardForInclude(t *testing.T) {
	p := "/Users/t/anything"
	m := tree(p + "/")
	own(m, p)
	_, _, problems := Apply(nil, m, exclusion.New(m), &Plan{Actions: []Action{{Op: OpInclude, Path: p}}})
	if len(problems) != 0 || len(m.Xattrs(p)) != 0 {
		t.Fatalf("problems %v xattrs %v", problems, m.Xattrs(p))
	}
}

func TestSizeCountsHardLinksOnce(t *testing.T) {
	m := tree()
	m.WriteFile("/d/a", make([]byte, 8192))
	m.Link("/d/a", "/d/sub/b")
	m.WriteFile("/d/c", make([]byte, 100))
	got, err := Size(m, "/d")
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(8192 + 4096); got != want {
		t.Fatalf("size = %d, want %d", got, want)
	}
}

func TestSizeDoesNotFollowSymlinks(t *testing.T) {
	m := tree()
	m.WriteFile("/big/f", make([]byte, 1<<20))
	m.WriteFile("/d/a", make([]byte, 4096))
	m.Symlink("/big", "/d/link")
	got, err := Size(m, "/d")
	if err != nil {
		t.Fatal(err)
	}
	// 4096 for a, plus the link's own (tiny) block.
	if got > 4096+4096 {
		t.Fatalf("followed the symlink: %d", got)
	}
}

func TestSizes(t *testing.T) {
	m := tree()
	m.WriteFile("/a/f", make([]byte, 4096))
	m.WriteFile("/b/f", make([]byte, 8192))
	got := Sizes(m, []string{"/a", "/b", "/missing"}, 4)
	if got["/a"] != 4096 || got["/b"] != 8192 {
		t.Fatalf("sizes = %v", got)
	}
	if _, ok := got["/missing"]; ok {
		t.Fatal("missing path sized")
	}
}

// A build can delete a folder between the walk and Apply: that's not a
// problem, and the hint goes.
func TestApplyVanishedPathIsSilent(t *testing.T) {
	a, b := D+"/a/node_modules", D+"/b/vendor"
	m := tree(a+"/", b+"/")
	r := resolved(t, m, nodeCfg)
	own(m, b)
	m.FailXattr(a, exclusion.MarkerName, syscall.ENOENT)
	m.FailXattr(b, exclusion.AttrName, syscall.ENOENT)
	plan := &Plan{Actions: []Action{{Op: OpExclude, Path: a}, {Op: OpInclude, Path: b}}, Hints: hints(a)}
	applied, hs, problems := Apply(r, m, exclusion.New(m), plan)
	if len(problems) != 0 || len(hs) != 0 || len(applied) != 0 {
		t.Fatalf("applied %v hints %v problems %+v", applied, hs, problems)
	}
}

package engine

import (
	"path"
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

func TestEvaluate(t *testing.T) {
	m := tree(
		D+"/app/node_modules/b/node_modules/",
		D+"/app/src/",
		D+"/app/.git/node_modules/",
		D+"/kept/node_modules/",
		D+"/locked/node_modules/",
		D+"/closed/",
		"/Users/t/.npm/",
		"/Users/t/Documents/node_modules/",
		"/Volumes/USB/Developer/x/node_modules/",
	)
	m.Deny(D+"/locked", syscall.EPERM) // parent of locked/node_modules can't be listed
	m.Deny(D+"/closed", syscall.EACCES)
	r := resolved(t, m, `version = 1
roots = ["~/Developer", "/Volumes/USB/Developer"]
skip = [".git"]
keep = ["~/Developer/kept"]
[[rule]]
name = "node"
names = ["node_modules"]
[[rule]]
name = "caches"
paths = ["~/.npm"]
`)
	m.Unmount("/Volumes/USB")
	cases := []struct {
		path      string
		reason    Reason
		exclude   bool
		rule      string
		guard     string
		coveredBy string
	}{
		{D + "/app/node_modules", ReasonMatched, true, "node", "", ""},
		{"/Users/t/.npm", ReasonFixed, true, "caches", "", ""},
		{D + "/app/src", ReasonNotMatched, false, "", "", ""},
		{D + "/app/.git/node_modules", ReasonSkipped, false, "", "", ""},
		{"/Users/t/Documents/node_modules", ReasonOutsideRoots, false, "", "", ""},
		{D + "/kept/node_modules", ReasonBlocked, false, "node", "is kept by " + D + "/kept", ""},
		{D, ReasonBlocked, false, "", "is a scan root", ""},
		{"/Users/t", ReasonBlocked, false, "", "is your home folder", ""},
		{D + "/closed", ReasonUnreadable, false, "", "", ""},
		{D + "/locked/node_modules", ReasonUnreadable, false, "", "", ""},
		{D + "/gone/node_modules", ReasonMissing, false, "", "", ""},
		{"/Volumes/USB/Developer/x/node_modules", ReasonUnmounted, false, "", "", ""},
		{D + "/app/node_modules/b/node_modules", ReasonMatched, true, "node", "", D + "/app/node_modules"},
	}
	for _, c := range cases {
		t.Run(strings.TrimPrefix(c.path, D), func(t *testing.T) {
			d := Evaluate(r, m, c.path, true)
			if d.Path != c.path || d.Reason != c.reason || d.Exclude != c.exclude || d.Match.Rule != c.rule || d.Guard != c.guard || d.CoveredBy != c.coveredBy {
				t.Fatalf("got %+v", d)
			}
			if c.reason == ReasonUnreadable && (d.Err == nil || !fsys.IsUnreadable(d.Err)) {
				t.Fatalf("unreadable without error: %+v", d)
			}
		})
	}
	if d := Evaluate(r, m, D+"/app/node_modules/b/node_modules", false); d.CoveredBy != "" {
		t.Fatalf("CoveredBy computed without asking: %+v", d)
	}
}

func TestEvaluateAgreesWithScan(t *testing.T) {
	m := tree(
		D+"/app/node_modules/x/node_modules/",
		D+"/app/package.json",
		D+"/svc/build.gradle",
		D+"/svc/build/classes/",
		D+"/svc/src/main/",
		D+"/docs/build/",
		D+"/venv1/pyvenv.cfg",
		D+"/venv1/lib/",
		D+"/c/Cargo.toml",
		D+"/c/target/debug/",
		D+"/repo/.git/objects/",
		D+"/notes/out/",
		D+"/notes/web/node_modules/",
		D+"/data/.nobackup",
	)
	r := resolved(t, m, strings.Replace(shipped("~/Developer"), "keep = []", `keep = ["~/Developer/notes"]`, 1))
	res := Scan(r, m, exclusion.New(m))
	var dirs []string
	var walk func(string)
	walk = func(p string) {
		es, _ := m.ReadDir(p)
		for _, e := range es {
			if e.Type == fsys.TypeDir {
				c := path.Join(p, e.Name)
				dirs = append(dirs, c)
				walk(c)
			}
		}
	}
	walk(D)
	for _, p := range dirs {
		d := Evaluate(r, m, p, true)
		_, inScan := res.Desired[p]
		if d.CoveredBy != "" {
			continue
		}
		if d.Exclude != inScan {
			t.Errorf("%s: Evaluate.Exclude=%v (%s) but in Scan.Desired=%v", p, d.Exclude, d.Reason, inScan)
		}
	}
	if len(res.Desired) == 0 {
		t.Fatal("fixture matched nothing")
	}
}

func TestEvaluateFolderVanishedAfterLstat(t *testing.T) {
	m := tree(D + "/app/node_modules/")
	r := resolved(t, m, nodeCfg)
	m.Deny(D+"/app/node_modules", syscall.ENOENT) // listing fails: it's gone
	if d := Evaluate(r, m, D+"/app/node_modules", false); d.Reason != ReasonMissing {
		t.Fatalf("got %+v", d)
	}
	m2 := tree(D + "/app/node_modules/")
	m2.Deny(D+"/app", syscall.ENOENT) // parent gone
	if d := Evaluate(r, m2, D+"/app/node_modules", false); d.Reason != ReasonMissing {
		t.Fatalf("parent gone: %+v", d)
	}
}

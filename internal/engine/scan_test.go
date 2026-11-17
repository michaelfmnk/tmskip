package engine

import (
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

const D = "/Users/t/Developer"

func desired(res *ScanResult) map[string]string {
	out := map[string]string{}
	for p, d := range res.Desired {
		out[p] = d.Match.Rule
	}
	return out
}

func blockedPaths(res *ScanResult) map[string]string {
	out := map[string]string{}
	for _, d := range res.Blocked {
		out[d.Path] = d.Guard
	}
	return out
}

func scan(t *testing.T, m *memfs.FS, src string) *ScanResult {
	t.Helper()
	r := resolved(t, m, src)
	return Scan(r, m, exclusion.New(m))
}

func TestScanMatching(t *testing.T) {
	cases := []struct {
		name  string
		files []string
		cfg   string
		want  map[string]string
	}{
		{"names hit, not descended", []string{D + "/app/node_modules/x/node_modules/", D + "/app/package.json"}, shipped("~/Developer"),
			map[string]string{D + "/app/node_modules": "node"}},
		{"names case-sensitive", []string{D + "/app/Node_Modules/"}, shipped("~/Developer"), map[string]string{}},
		{"contains any name", []string{D + "/kokoro-env/pyvenv.cfg"}, shipped("~/Developer"),
			map[string]string{D + "/kokoro-env": "python-venv"}},
		{"contains is direct child only", []string{D + "/a/b/pyvenv.cfg"}, shipped("~/Developer"),
			map[string]string{D + "/a/b": "python-venv"}},
		{"parent_has gate", []string{D + "/svc/build.gradle", D + "/svc/build/"}, shipped("~/Developer"),
			map[string]string{D + "/svc/build": "jvm-build"}},
		{"parent_has missing", []string{D + "/docs/build/"}, shipped("~/Developer"), map[string]string{}},
		{"rust and first rule wins", []string{D + "/c/Cargo.toml", D + "/c/target/", D + "/m/pom.xml", D + "/m/target/"}, shipped("~/Developer"),
			map[string]string{D + "/c/target": "rust", D + "/m/target": "jvm-build"}},
		{"swiftpm", []string{D + "/p/Package.swift", D + "/p/.build/"}, shipped("~/Developer"),
			map[string]string{D + "/p/.build": "swiftpm"}},
		{"nobackup", []string{D + "/data/.nobackup"}, shipped("~/Developer"), map[string]string{D + "/data": "nobackup-marker"}},
		{"disabled rule", []string{D + "/app/node_modules/"}, `version = 1
roots = ["~/Developer"]
[[rule]]
name = "node"
names = ["node_modules"]
enabled = false`, map[string]string{}},
		{"fixed path exists / missing", []string{"/Users/t/.npm/x", D + "/"}, shipped("~/Developer"),
			map[string]string{"/Users/t/.npm": "dev-caches"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := scan(t, tree(c.files...), c.cfg)
			if got := desired(res); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("desired = %v, want %v", got, c.want)
			}
			if len(res.Problems) != 0 {
				t.Fatalf("problems: %+v", res.Problems)
			}
		})
	}
}

func TestScanFixedViaSymlink(t *testing.T) {
	m := tree("/Volumes/X/models/a.gguf")
	m.Symlink("/Volumes/X/models", "/Users/t/.lmstudio/models")
	res := scan(t, m, `version = 1
[[rule]]
name = "my-models"
paths = ["~/.lmstudio/models"]`)
	if got := desired(res); !reflect.DeepEqual(got, map[string]string{"/Volumes/X/models": "my-models"}) {
		t.Fatalf("desired = %v", got)
	}
}

func TestScanNotesOutNearMiss(t *testing.T) {
	files := []string{D + "/notes/.idea/", D + "/notes/out/report.pdf", D + "/notes/web/node_modules/"}
	cases := []struct {
		name    string
		cfg     string
		desired map[string]string
		blocked map[string]string
	}{
		{"shipped groups", strings.Replace(shipped("~/Developer"), `name  = "node"`, `name  = "node"
enabled = false`, 1), map[string]string{}, map[string]string{}},
		{"user rule with gradle gate", `version = 1
roots = ["~/Developer"]
[[rule]]
name = "out"
names = ["out"]
parent_has = ["build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts", "pom.xml"]`, map[string]string{}, map[string]string{}},
		{"keep protects", `version = 1
roots = ["~/Developer"]
keep = ["~/Developer/notes"]
[[rule]]
name = "out"
names = ["out"]
[[rule]]
name = "node"
names = ["node_modules"]`, map[string]string{}, map[string]string{
			D + "/notes/out":              "is kept by " + D + "/notes",
			D + "/notes/web/node_modules": "is kept by " + D + "/notes",
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := tree(files...)
			res := scan(t, m, c.cfg)
			if got := desired(res); !reflect.DeepEqual(got, c.desired) {
				t.Fatalf("desired = %v", got)
			}
			if got := blockedPaths(res); !reflect.DeepEqual(got, c.blocked) {
				t.Fatalf("blocked = %v", got)
			}
			if len(c.blocked) > 0 && !contains(m.ReadDirCalls(), D+"/notes/out") {
				t.Fatal("blocked match was not walked into")
			}
		})
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func TestScanSkipsGit(t *testing.T) {
	res := scan(t, tree(D+"/app/.git/node_modules/", D+"/.git/"), shipped("~/Developer"))
	if len(res.Desired) != 0 {
		t.Fatalf("desired = %v", desired(res))
	}
}

func TestScanSymlinkNotFollowed(t *testing.T) {
	m := tree(D+"/app/", "/Users/t/elsewhere/node_modules/")
	m.Symlink("/Users/t/elsewhere/node_modules", D+"/app/node_modules")
	m.Symlink("/Users/t/elsewhere", D+"/app/link")
	res := scan(t, m, shipped("~/Developer"))
	if len(res.Desired) != 0 {
		t.Fatalf("desired = %v", desired(res))
	}
	for _, c := range m.ReadDirCalls() {
		if strings.Contains(c, "elsewhere") || strings.HasSuffix(c, "/link") {
			t.Fatalf("followed a symlink: %s", c)
		}
	}
}

func TestScanUnreadable(t *testing.T) {
	m := tree(D+"/a/", D+"/b/node_modules/", D+"/c/", D+"/a/node_modules/")
	m.Deny(D+"/a", syscall.EACCES)
	m.Deny(D+"/c", syscall.EPERM)
	res := scan(t, m, shipped("~/Developer"))
	if got := desired(res); !reflect.DeepEqual(got, map[string]string{D + "/b/node_modules": "node"}) {
		t.Fatalf("siblings not scanned: %v", got)
	}
	sort.Slice(res.Problems, func(i, j int) bool { return res.Problems[i].Path < res.Problems[j].Path })
	if len(res.Problems) != 2 || res.Problems[0].Path != D+"/a" || res.Problems[0].Denied ||
		res.Problems[1].Path != D+"/c" || !res.Problems[1].Denied {
		t.Fatalf("problems = %+v", res.Problems)
	}
}

func TestScanRootNamedBuildNeverExcluded(t *testing.T) {
	m := tree("/Users/t/svc/build.gradle", "/Users/t/svc/build/x/")
	res := scan(t, m, shipped("~/svc/build"))
	if len(res.Desired) != 0 {
		t.Fatalf("desired = %v", desired(res))
	}
}

func TestScanMarkerFoundDeep(t *testing.T) {
	m := tree(D + "/a/b/c/old/")
	m.SetXattr(D+"/a/b/c/old", exclusion.MarkerName, []byte("1"))
	m.SetXattr(D, exclusion.MarkerName, []byte("1"))
	res := scan(t, m, shipped("~/Developer"))
	sort.Strings(res.MarkedSeen)
	if !reflect.DeepEqual(res.MarkedSeen, []string{D, D + "/a/b/c/old"}) {
		t.Fatalf("marked = %v", res.MarkedSeen)
	}
}

func TestScanOverlappingRootsWalkedOnce(t *testing.T) {
	m := tree(D + "/work/deep/")
	scan(t, m, shipped("~/Developer", "~/Developer/work"))
	n := 0
	for _, c := range m.ReadDirCalls() {
		if c == D+"/work/deep" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("deep listed %d times", n)
	}
}

func TestScanRootMissing(t *testing.T) {
	res := scan(t, tree(), shipped("~/Develper"))
	if len(res.Problems) != 0 || len(res.Desired) != 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestScanFolderVanishesMidWalk(t *testing.T) {
	m := tree(D+"/app/build/", D+"/app/build.gradle", D+"/zzz/node_modules/")
	m.Deny(D+"/app/build", syscall.ENOENT) // listed by the parent, gone when read
	res := scan(t, m, shipped("~/Developer"))
	if len(res.Problems) != 0 {
		t.Fatalf("problems = %+v", res.Problems)
	}
	if got := desired(res); !reflect.DeepEqual(got, map[string]string{D + "/zzz/node_modules": "node"}) {
		t.Fatalf("desired = %v", got)
	}
}

func TestScanMatchedFolderNotDescended(t *testing.T) {
	m := tree(D+"/app/node_modules/a/b/", D+"/app/node_modules/c/")
	scan(t, m, shipped("~/Developer"))
	for _, c := range m.ReadDirCalls() {
		if strings.HasPrefix(c, D+"/app/node_modules/") {
			t.Fatalf("descended into a matched folder: %s", c)
		}
	}
}

func TestScanFixedPathUnderRootNotDescended(t *testing.T) {
	m := tree(D+"/models/a/b/", D+"/models/node_modules/")
	res := scan(t, m, `version = 1
roots = ["~/Developer"]
[[rule]]
name = "models"
paths = ["~/Developer/models"]
[[rule]]
name = "node"
names = ["node_modules"]`)
	if got := desired(res); !reflect.DeepEqual(got, map[string]string{D + "/models": "models"}) {
		t.Fatalf("desired = %v", got)
	}
	if res.Desired[D+"/models"].Reason != ReasonFixed {
		t.Fatalf("reason = %v", res.Desired[D+"/models"].Reason)
	}
	for _, c := range m.ReadDirCalls() {
		if strings.HasPrefix(c, D+"/models/") {
			t.Fatalf("descended into a fixed path: %s", c)
		}
	}
}

// A folder named in skip is neither matched nor walked into, even when a
// rule would match it.
func TestScanSkipBeatsRule(t *testing.T) {
	m := tree(D+"/app/node_modules/x/node_modules/", D+"/app/src/")
	res := scan(t, m, `version = 1
roots = ["~/Developer"]
skip = ["node_modules"]
[[rule]]
name = "node"
names = ["node_modules"]`)
	if len(res.Desired) != 0 || len(res.Blocked) != 0 {
		t.Fatalf("desired %v blocked %v", desired(res), res.Blocked)
	}
	for _, c := range m.ReadDirCalls() {
		if strings.Contains(c, "node_modules") {
			t.Fatalf("walked into a skipped folder: %s", c)
		}
	}
}

//go:build integration

package exclusion

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// safeTempDir returns a fresh temp dir and refuses to continue unless it is
// under the system temp dir and outside $HOME. Real exclusions are only ever
// written here.
func safeTempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		if h, err := filepath.EvalSymlinks(home); err == nil {
			home = h
		}
	}
	if !fsys.StrictlyUnder(d, tmp) || (home != "" && fsys.Under(d, home)) {
		t.Fatalf("refusing: temp dir %s is not under %s or is under $HOME", d, tmp)
	}
	return d
}

func tmutil(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("/usr/bin/tmutil", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("tmutil %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func mkdir(t *testing.T, d, name string) string {
	t.Helper()
	p := filepath.Join(d, name)
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestIntegrationTmutilEquivalence(t *testing.T) {
	d := safeTempDir(t)
	s := New(fsys.OS{})

	// (a) tmutil addexclusion (sticky, no -p) writes exactly Value.
	a := mkdir(t, d, "A")
	tmutil(t, "addexclusion", a)
	t.Cleanup(func() { exec.Command("/usr/bin/tmutil", "removeexclusion", a).Run() })
	got, err := fsys.OS{}.GetXattr(a, AttrName)
	if err != nil {
		t.Fatalf("read tmutil's xattr: %v", err)
	}
	if !bytes.Equal(got, Value) {
		t.Fatalf("tmutil wrote % X\nwe write     % X", got, Value)
	}

	// Every temp location (/private/var/folders, /private/tmp) is already a
	// Time Machine system exclusion, so `tmutil isexcluded` prints
	// [Excluded] for any folder there and can't tell sticky exclusions
	// apart. Equivalence is therefore shown through tmutil's own sticky
	// add/remove, which act on exactly this xattr.

	// (b) tmutil recognises our exclusion as its sticky exclusion and
	// removes it; our Include removes tmutil's.
	b := mkdir(t, d, "B")
	if err := s.Exclude(b); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/tmutil", "removeexclusion", b).Run() })
	if out := tmutil(t, "isexcluded", b); !strings.Contains(out, "[Excluded]") {
		t.Fatalf("after Exclude: %s", out)
	}
	tmutil(t, "removeexclusion", b)
	if ok, err := s.IsExcluded(b); ok || err != nil {
		t.Fatalf("tmutil removeexclusion left our exclusion: %v %v", ok, err)
	}
	tmutil(t, "addexclusion", b)
	if ok, err := s.IsExcluded(b); !ok || err != nil {
		t.Fatalf("tmutil's exclusion not seen: %v %v", ok, err)
	}
	if err := s.Include(b); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.IsExcluded(b); ok || err != nil {
		t.Fatalf("Include left tmutil's exclusion: %v %v", ok, err)
	}

	// (c) the marker doesn't interfere: tmutil sees and removes the
	// exclusion of a marked folder and leaves the marker alone.
	c := mkdir(t, d, "C")
	if err := s.Mark(c); err != nil {
		t.Fatal(err)
	}
	if err := s.Exclude(c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command("/usr/bin/tmutil", "removeexclusion", c).Run() })
	if out := tmutil(t, "isexcluded", c); !strings.Contains(out, "[Excluded]") {
		t.Fatalf("marked+excluded: %s", out)
	}
	tmutil(t, "removeexclusion", c)
	if ok, err := s.IsExcluded(c); ok || err != nil {
		t.Fatalf("tmutil didn't remove a marked folder's exclusion: %v %v", ok, err)
	}
	if ok, err := s.IsMarked(c); !ok || err != nil {
		t.Fatalf("marker lost: %v %v", ok, err)
	}
	s.Include(c)
	s.Unmark(c)
}

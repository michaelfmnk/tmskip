package fsys_test

import (
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

func canon(t *testing.T, fs fsys.FS, p string) (string, bool) {
	t.Helper()
	c, ok, err := fsys.Canonical(fs, p)
	if err != nil {
		t.Fatalf("Canonical(%q): %v", p, err)
	}
	return c, ok
}

func TestCanonicalFixesCaseOnCaseInsensitive(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	m.Mkdirs("/Users/t/Developer")
	got, ok := canon(t, m, "/users/T/developer")
	if got != "/Users/t/Developer" || !ok {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestCanonicalNeverLowercases(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/r/Dev")
	m.Mkdirs("/r/dev")
	for _, p := range []string{"/r/Dev", "/r/dev"} {
		if got, ok := canon(t, m, p); got != p || !ok {
			t.Fatalf("%q → %q %v", p, got, ok)
		}
	}
	// No unique case-folded match on a case-sensitive disk with both: typed name kept.
	if got, ok := canon(t, m, "/r/DEV"); got != "/r/DEV" || ok {
		t.Fatalf("/r/DEV → %q %v", got, ok)
	}
}

func TestCanonicalResolvesSymlinkChain(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Volumes/Data/models/llm")
	m.Symlink("/Volumes/Data", "/Users/t/data")   // absolute
	m.Symlink("data/models", "/Users/t/mdl")      // relative, through another link
	m.Symlink("../mdl", "/Users/t/sub/up")        // relative with ..
	got, ok := canon(t, m, "/Users/t/sub/up/llm") // link in the middle of the path
	if got != "/Volumes/Data/models/llm" || !ok {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestCanonicalMissingTailKeptAsTyped(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	m.Mkdirs("/Users/t/Developer")
	got, ok := canon(t, m, "/Users/t/developer/Nope/x")
	if got != "/Users/t/Developer/Nope/x" || ok {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestCanonicalLoopErrors(t *testing.T) {
	m := memfs.New()
	m.Symlink("/r/b", "/r/a")
	m.Symlink("/r/a", "/r/b")
	_, _, err := fsys.Canonical(m, "/r/a/x")
	if err == nil || !strings.Contains(err.Error(), "too many symlinks") {
		t.Fatalf("err = %v", err)
	}
}

func TestCanonicalUnreadableParentKeepsTypedName(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	m.Mkdirs("/Users/t/Documents/Proj")
	m.Deny("/Users/t/Documents", syscall.EPERM)
	got, ok := canon(t, m, "/Users/t/Documents/proj")
	if got != "/Users/t/Documents/proj" || !ok {
		t.Fatalf("got %q %v", got, ok)
	}
}

func TestCanonicalSpacesAndNonASCII(t *testing.T) {
	m := memfs.New(memfs.CaseInsensitive())
	for _, p := range []string{"/Users/t/Library/Developer/Xcode/iOS DeviceSupport", "/Users/t/Проєкти/app"} {
		m.Mkdirs(p)
		if got, ok := canon(t, m, p); got != p || !ok {
			t.Fatalf("%q → %q %v", p, got, ok)
		}
	}
	if got, _ := canon(t, m, "/Users/t/проєкти/app"); got != "/Users/t/Проєкти/app" {
		t.Fatalf("non-ASCII case fold → %q", got)
	}
}

func TestCanonicalRootAndCleaning(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/a/b")
	if got, ok := canon(t, m, "/"); got != "/" || !ok {
		t.Fatalf("/ → %q", got)
	}
	if got, ok := canon(t, m, "/a//b/"); got != "/a/b" || !ok {
		t.Fatalf("/a//b/ → %q", got)
	}
	if _, _, err := fsys.Canonical(m, "relative/x"); err == nil {
		t.Fatal("relative path accepted")
	}
}

func TestUnder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"/a/bc", "/a/b", false},
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b", true},
		{"/a", "/a/b", false},
		{"/a", "/", true},
	}
	for _, c := range cases {
		if got := fsys.Under(c.a, c.b); got != c.want {
			t.Errorf("Under(%q, %q) = %v", c.a, c.b, got)
		}
	}
	if fsys.StrictlyUnder("/a/b", "/a/b") || !fsys.StrictlyUnder("/a/b/c", "/a/b") {
		t.Error("StrictlyUnder")
	}
}

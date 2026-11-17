package memfs

import (
	"bytes"
	"errors"
	"reflect"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

var _ fsys.FS = (*FS)(nil)

func names(es []fsys.Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestMemReadDirSortedWithTypes(t *testing.T) {
	m := New()
	m.Mkdirs("/r/b-dir")
	m.WriteFile("/r/a-file", []byte("x"))
	m.Symlink("/r/b-dir", "/r/c-link")
	es, err := m.ReadDir("/r")
	if err != nil {
		t.Fatal(err)
	}
	want := []fsys.Entry{{Name: "a-file", Type: fsys.TypeFile}, {Name: "b-dir", Type: fsys.TypeDir}, {Name: "c-link", Type: fsys.TypeSymlink}}
	if !reflect.DeepEqual(es, want) {
		t.Fatalf("entries = %+v", es)
	}
	// ReadDir through a symlink lists the target.
	if _, err := m.ReadDir("/r/c-link"); err != nil {
		t.Fatalf("readdir via link: %v", err)
	}
	if _, err := m.ReadDir("/r/a-file"); !errors.Is(err, syscall.ENOTDIR) {
		t.Fatalf("readdir on file: %v", err)
	}
	if _, err := m.ReadDir("/r/nope"); !fsys.IsNotExist(err) {
		t.Fatalf("readdir missing: %v", err)
	}
}

func TestMemLstatDoesNotFollowSymlink(t *testing.T) {
	m := New()
	m.Mkdirs("/r/d")
	m.Symlink("d", "/r/link")
	info, err := m.Lstat("/r/link")
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != fsys.TypeSymlink {
		t.Fatalf("type = %v", info.Type)
	}
	target, err := m.Readlink("/r/link")
	if err != nil || target != "d" {
		t.Fatalf("readlink = %q, %v", target, err)
	}
	// Intermediate symlinks are followed.
	m.Mkdirs("/r/d/inner")
	if info, err := m.Lstat("/r/link/inner"); err != nil || info.Type != fsys.TypeDir {
		t.Fatalf("lstat through link = %+v, %v", info, err)
	}
}

func TestMemCaseInsensitiveLookupKeepsStoredName(t *testing.T) {
	m := New(CaseInsensitive())
	m.Mkdirs("/a/Dev")
	if _, err := m.Lstat("/a/DEV"); err != nil {
		t.Fatalf("lstat other case: %v", err)
	}
	es, _ := m.ReadDir("/a")
	if got := names(es); !reflect.DeepEqual(got, []string{"Dev"}) {
		t.Fatalf("names = %v", got)
	}
	m.Mkdirs("/a/dev/x") // same folder
	es, _ = m.ReadDir("/a")
	if got := names(es); !reflect.DeepEqual(got, []string{"Dev"}) {
		t.Fatalf("names after mkdir other case = %v", got)
	}

	cs := New()
	cs.Mkdirs("/a/Dev")
	if _, err := cs.Lstat("/a/dev"); !fsys.IsNotExist(err) {
		t.Fatalf("case-sensitive lookup should miss: %v", err)
	}
}

func TestMemDenyReturnsEPERM(t *testing.T) {
	m := New()
	m.Mkdirs("/Users/t/Documents/x")
	m.Deny("/Users/t/Documents", syscall.EPERM)
	_, err := m.ReadDir("/Users/t/Documents")
	if !fsys.IsDenied(err) || !fsys.IsUnreadable(err) {
		t.Fatalf("err = %v", err)
	}
	if _, err := m.GetXattr("/Users/t/Documents", "a"); !fsys.IsDenied(err) {
		t.Fatalf("getxattr err = %v", err)
	}
	if err := m.Probe("/Users/t/Documents"); !fsys.IsDenied(err) {
		t.Fatalf("probe err = %v", err)
	}
	if _, err := m.Lstat("/Users/t/Documents"); err != nil {
		t.Fatalf("lstat of denied dir should work: %v", err)
	}
	m.Deny("/Users/t/Documents/x", syscall.EACCES)
	_, err = m.ReadDir("/Users/t/Documents/x")
	if fsys.IsDenied(err) || !fsys.IsUnreadable(err) {
		t.Fatalf("EACCES err = %v", err)
	}
}

func TestMemXattrRoundTripAndNoAttr(t *testing.T) {
	m := New()
	m.Mkdirs("/r/d")
	if _, err := m.GetXattr("/r/d", "n"); !errors.Is(err, fsys.ErrNoAttr) {
		t.Fatalf("absent: %v", err)
	}
	if err := m.RemoveXattr("/r/d", "n"); err != nil {
		t.Fatalf("remove absent: %v", err)
	}
	if err := m.SetXattr("/r/d", "n", []byte("1")); err != nil {
		t.Fatal(err)
	}
	v, err := m.GetXattr("/r/d", "n")
	if err != nil || string(v) != "1" {
		t.Fatalf("get = %q, %v", v, err)
	}
	if err := m.RemoveXattr("/r/d", "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.GetXattr("/r/d", "n"); !errors.Is(err, fsys.ErrNoAttr) {
		t.Fatalf("after remove: %v", err)
	}
	want := []string{"set n /r/d", "rm n /r/d"}
	if !reflect.DeepEqual(m.Ops(), want) {
		t.Fatalf("ops = %v", m.Ops())
	}
	// xattrs never follow a symlink.
	m.Symlink("/r/d", "/r/l")
	_ = m.SetXattr("/r/l", "n", []byte("1"))
	if len(m.Xattrs("/r/d")) != 0 {
		t.Fatalf("xattr followed the link: %v", m.Xattrs("/r/d"))
	}
	m.FailXattr("/r/d", "n", syscall.EPERM)
	if _, err := m.GetXattr("/r/d", "n"); !fsys.IsDenied(err) {
		t.Fatalf("failxattr: %v", err)
	}
	if err := m.SetXattr("/r/d", "n", []byte("1")); !fsys.IsDenied(err) {
		t.Fatalf("failxattr set: %v", err)
	}
}

func TestMemVolumeMounted(t *testing.T) {
	m := New()
	m.Mkdirs("/Volumes/USB/x")
	m.Mkdirs("/Users/t/x")
	if !m.VolumeMounted("/Volumes/USB/x") {
		t.Fatal("mounted volume reported unmounted")
	}
	m.Unmount("/Volumes/USB")
	if m.VolumeMounted("/Volumes/USB/x") {
		t.Fatal("unmounted volume reported mounted")
	}
	if _, err := m.Lstat("/Volumes/USB/x"); !fsys.IsNotExist(err) {
		t.Fatalf("unmounted path exists: %v", err)
	}
	if !m.VolumeMounted("/Users/t/x") {
		t.Fatal("boot volume reported unmounted")
	}
}

func TestMemWriteFileAtomicAndMkdirAll(t *testing.T) {
	m := New()
	if err := m.MkdirAll("/a/b/c", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.WriteFileAtomic("/a/b/c/f", []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := m.ReadFile("/a/b/c/f")
	if err != nil || !bytes.Equal(got, []byte("hello")) {
		t.Fatalf("read = %q, %v", got, err)
	}
	info, _ := m.Lstat("/a/b/c/f")
	if info.Mode.Perm() != 0o600 || info.Size != 5 || info.Blocks == 0 {
		t.Fatalf("info = %+v", info)
	}
	if err := m.WriteFileAtomic("/missing/f", nil, 0o644); !fsys.IsNotExist(err) {
		t.Fatalf("write into missing dir: %v", err)
	}
	if err := m.AppendFile("/a/b/c/f", []byte(" world")); err != nil {
		t.Fatal(err)
	}
	got, _ = m.ReadFile("/a/b/c/f")
	if string(got) != "hello world" {
		t.Fatalf("append = %q", got)
	}
	if err := m.Rename("/a/b/c/f", "/a/g"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Lstat("/a/b/c/f"); !fsys.IsNotExist(err) {
		t.Fatal("rename left source")
	}
	if err := m.RemoveAll("/a/b"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Lstat("/a/b"); !fsys.IsNotExist(err) {
		t.Fatal("removeall left dir")
	}
	if err := m.RemoveAll("/nope"); err != nil {
		t.Fatalf("removeall missing: %v", err)
	}
}

func TestMemHardLinkSharesInode(t *testing.T) {
	m := New()
	m.WriteFile("/a/f", make([]byte, 10000))
	m.Link("/a/f", "/a/g")
	fi, _ := m.Lstat("/a/f")
	gi, _ := m.Lstat("/a/g")
	if fi.Ino != gi.Ino || fi.Dev != gi.Dev || fi.Nlink != 2 {
		t.Fatalf("f=%+v g=%+v", fi, gi)
	}
}

// DenySearch is a folder without the search (x) bit, or a TCC-protected one:
// the folder itself can be lstat'ed, but it can't be listed and nothing
// below it can be reached.
func TestMemDenySearch(t *testing.T) {
	m := New()
	m.Mkdirs("/Users/t/Private/x/y")
	m.DenySearch("/Users/t/Private", syscall.EACCES)
	if _, err := m.Lstat("/Users/t/Private"); err != nil {
		t.Fatalf("lstat of the folder itself: %v", err)
	}
	for _, p := range []string{"/Users/t/Private/x", "/Users/t/Private/x/y"} {
		if _, err := m.Lstat(p); !errors.Is(err, syscall.EACCES) {
			t.Fatalf("lstat %s: %v", p, err)
		}
	}
	if _, err := m.ReadDir("/Users/t/Private"); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("readdir: %v", err)
	}
	if _, err := m.GetXattr("/Users/t/Private/x", "n"); !errors.Is(err, syscall.EACCES) {
		t.Fatalf("getxattr below: %v", err)
	}
	if _, err := m.ReadDir("/Users/t"); err != nil {
		t.Fatalf("parent listing: %v", err)
	}
}

//go:build darwin

package fsys

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Only the marker xattr name is ever used here — never the Time Machine
// exclusion attribute.
const testMarker = "dev.tmskip.managed"

func tempDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestOSReadDirTypes(t *testing.T) {
	d := tempDir(t)
	os.Mkdir(filepath.Join(d, "b"), 0o755)
	os.WriteFile(filepath.Join(d, "a"), []byte("x"), 0o644)
	os.Symlink("b", filepath.Join(d, "c"))
	es, err := OS{}.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{"a", TypeFile}, {"b", TypeDir}, {"c", TypeSymlink}}
	if !reflect.DeepEqual(es, want) {
		t.Fatalf("entries = %+v", es)
	}
	if _, err := (OS{}).ReadDir(filepath.Join(d, "nope")); !IsNotExist(err) {
		t.Fatalf("missing: %v", err)
	}
}

func TestOSLstatSymlinkAndBlocks(t *testing.T) {
	d := tempDir(t)
	f := filepath.Join(d, "f")
	os.WriteFile(f, make([]byte, 10*1024), 0o640)
	os.Symlink("f", filepath.Join(d, "l"))
	os.Link(f, filepath.Join(d, "h"))
	info, err := OS{}.Lstat(f)
	if err != nil {
		t.Fatal(err)
	}
	if info.Type != TypeFile || info.Size != 10*1024 || info.Blocks <= 0 || info.Nlink != 2 || info.Ino == 0 || info.Mode.Perm() != 0o640 {
		t.Fatalf("info = %+v", info)
	}
	li, err := OS{}.Lstat(filepath.Join(d, "l"))
	if err != nil || li.Type != TypeSymlink {
		t.Fatalf("link info = %+v, %v", li, err)
	}
	target, err := OS{}.Readlink(filepath.Join(d, "l"))
	if err != nil || target != "f" {
		t.Fatalf("readlink = %q, %v", target, err)
	}
}

func TestOSMarkerXattrRoundTrip(t *testing.T) {
	d := tempDir(t)
	p := filepath.Join(d, "x")
	os.Mkdir(p, 0o755)
	fs := OS{}
	if _, err := fs.GetXattr(p, testMarker); !errors.Is(err, ErrNoAttr) {
		t.Fatalf("absent: %v", err)
	}
	if err := fs.SetXattr(p, testMarker, []byte("1")); err != nil {
		t.Fatal(err)
	}
	v, err := fs.GetXattr(p, testMarker)
	if err != nil || string(v) != "1" {
		t.Fatalf("get = %q, %v", v, err)
	}
	if err := fs.RemoveXattr(p, testMarker); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.GetXattr(p, testMarker); !errors.Is(err, ErrNoAttr) {
		t.Fatalf("after remove: %v", err)
	}
	if err := fs.RemoveXattr(p, testMarker); err != nil {
		t.Fatalf("remove absent: %v", err)
	}
}

func TestOSXattrDoesNotFollowSymlink(t *testing.T) {
	d := tempDir(t)
	target := filepath.Join(d, "t")
	link := filepath.Join(d, "l")
	os.Mkdir(target, 0o755)
	os.Symlink(target, link)
	if err := (OS{}).SetXattr(link, testMarker, []byte("1")); err != nil {
		t.Fatal(err)
	}
	if _, err := (OS{}).GetXattr(target, testMarker); !errors.Is(err, ErrNoAttr) {
		t.Fatalf("target got the xattr: %v", err)
	}
	if _, err := (OS{}).GetXattr(link, testMarker); err != nil {
		t.Fatalf("link itself should carry it: %v", err)
	}
}

func TestOSPermissionDeniedIsUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	d := tempDir(t)
	p := filepath.Join(d, "locked")
	os.Mkdir(p, 0o755)
	os.Chmod(p, 0)
	t.Cleanup(func() { os.Chmod(p, 0o755) })
	_, err := OS{}.ReadDir(p)
	if !IsUnreadable(err) || IsDenied(err) {
		t.Fatalf("err = %v", err)
	}
}

func TestOSVolumeMountedBootVolume(t *testing.T) {
	if !(OS{}).VolumeMounted(tempDir(t)) {
		t.Fatal("temp dir reported unmounted")
	}
	if (OS{}).VolumeMounted("/Volumes/tmskip-no-such-volume-xyz/a") {
		t.Fatal("missing volume reported mounted")
	}
}

func TestOSFileOps(t *testing.T) {
	d := tempDir(t)
	fs := OS{}
	p := filepath.Join(d, "a", "b")
	if err := fs.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(p, "f")
	if err := fs.WriteFileAtomic(f, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fs.WriteFileAtomic(f, []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fs.AppendFile(f, []byte("+")); err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(f)
	if err != nil || string(got) != "two+" {
		t.Fatalf("read = %q, %v", got, err)
	}
	info, _ := fs.Lstat(f)
	if info.Mode.Perm() != 0o600 {
		t.Fatalf("mode = %v", info.Mode)
	}
	es, _ := fs.ReadDir(p)
	if len(es) != 1 {
		t.Fatalf("temp file left behind: %+v", es)
	}
	if err := fs.Probe(f); err != nil {
		t.Fatal(err)
	}
	if err := fs.Rename(f, filepath.Join(d, "g")); err != nil {
		t.Fatal(err)
	}
	if err := fs.RemoveAll(filepath.Join(d, "a")); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.Lstat(filepath.Join(d, "a")); !IsNotExist(err) {
		t.Fatalf("removeall: %v", err)
	}
}

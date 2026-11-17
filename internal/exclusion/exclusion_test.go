package exclusion

import (
	"bytes"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

func TestValueIsBplistOfBackupd(t *testing.T) {
	if len(Value) != 61 {
		t.Fatalf("len = %d", len(Value))
	}
	if !bytes.HasPrefix(Value, []byte("bplist00")) {
		t.Fatal("not a bplist00")
	}
	if !bytes.Contains(Value, []byte("com.apple.backupd")) {
		t.Fatal("missing com.apple.backupd")
	}
	if Value[len(Value)-1] != 0x1C {
		t.Fatalf("last byte = %#x", Value[len(Value)-1])
	}
	if AttrName != "com.apple.metadata:com_apple_backup_excludeItem" || MarkerName != "dev.tmskip.managed" || string(MarkerValue) != "1" {
		t.Fatal("names changed")
	}
}

func TestExcludeWritesExactBytes(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/r/d")
	s := New(m)
	if err := s.Exclude("/r/d"); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark("/r/d"); err != nil {
		t.Fatal(err)
	}
	x := m.Xattrs("/r/d")
	if !bytes.Equal(x[AttrName], Value) {
		t.Fatalf("exclusion = %x", x[AttrName])
	}
	if !bytes.Equal(x[MarkerName], []byte("1")) {
		t.Fatalf("marker = %q", x[MarkerName])
	}
}

func TestPresenceMeansTrue(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/r/d")
	s := New(m)
	for _, f := range []func(string) (bool, error){s.IsExcluded, s.IsMarked} {
		if ok, err := f("/r/d"); ok || err != nil {
			t.Fatalf("absent → %v %v", ok, err)
		}
	}
	m.SetXattr("/r/d", AttrName, []byte("anything"))
	m.SetXattr("/r/d", MarkerName, []byte("x"))
	for _, f := range []func(string) (bool, error){s.IsExcluded, s.IsMarked} {
		if ok, err := f("/r/d"); !ok || err != nil {
			t.Fatalf("present → %v %v", ok, err)
		}
	}
}

func TestIncludeAndUnmarkIdempotent(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/r/d")
	s := New(m)
	s.Mark("/r/d")
	s.Exclude("/r/d")
	for i := 0; i < 2; i++ {
		if err := s.Include("/r/d"); err != nil {
			t.Fatal(err)
		}
		if err := s.Unmark("/r/d"); err != nil {
			t.Fatal(err)
		}
	}
	if len(m.Xattrs("/r/d")) != 0 {
		t.Fatalf("xattrs left: %v", m.Xattrs("/r/d"))
	}
}

func TestReadErrorPropagates(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/r/d")
	m.FailXattr("/r/d", MarkerName, syscall.EPERM)
	_, err := New(m).IsMarked("/r/d")
	if err == nil || !fsys.IsDenied(err) {
		t.Fatalf("err = %v", err)
	}
}

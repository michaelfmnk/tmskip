// Package fsys is tmskip's view of the filesystem: a small interface that the
// OS implementation and the in-memory fake (memfs) both satisfy, plus path
// canonicalisation and error classification.
package fsys

import (
	"os"
	"strings"
)

// Type is a directory entry's kind, as reported by readdir (d_type) or lstat.
type Type uint8

const (
	TypeDir Type = iota + 1
	TypeFile
	TypeSymlink
	TypeOther
)

func (t Type) String() string {
	switch t {
	case TypeDir:
		return "dir"
	case TypeFile:
		return "file"
	case TypeSymlink:
		return "symlink"
	case TypeOther:
		return "other"
	}
	return "unknown"
}

// Entry is one name in a directory listing; no stat is done.
type Entry struct {
	Name string
	Type Type
}

// Info is the result of Lstat.
type Info struct {
	Type   Type
	Mode   os.FileMode // permission bits
	Size   int64
	Blocks int64 // 512-byte blocks allocated
	Dev    uint64
	Ino    uint64
	Nlink  uint64
}

// FS is every filesystem operation tmskip performs.
type FS interface {
	Lstat(path string) (Info, error)
	ReadDir(path string) ([]Entry, error) // sorted by name
	Readlink(path string) (string, error)
	GetXattr(path, name string) ([]byte, error)     // ErrNoAttr when absent; never follows symlinks
	SetXattr(path, name string, value []byte) error // XATTR_NOFOLLOW
	RemoveXattr(path, name string) error            // absent → nil
	ReadFile(path string) ([]byte, error)
	WriteFileAtomic(path string, data []byte, perm os.FileMode) error // temp in same dir + rename
	AppendFile(path string, data []byte) error                        // log only
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldpath, newpath string) error
	RemoveAll(path string) error
	Probe(path string) error // open O_RDONLY + close (FDA probe)
	VolumeMounted(path string) bool
}

// VolumeMountedVia implements FS.VolumeMounted on top of an lstat function:
// a path under /Volumes/<name>/… is on a mounted volume iff /Volumes/<name>
// exists and lives on a different device than /Volumes itself. Any other
// path is on the boot volume.
func VolumeMountedVia(lstat func(string) (Info, error), p string) bool {
	const vols = "/Volumes/"
	if !strings.HasPrefix(p, vols) {
		return true
	}
	name, _, _ := strings.Cut(p[len(vols):], "/")
	if name == "" {
		return true
	}
	vi, err := lstat(vols + name)
	if err != nil {
		return false
	}
	if vi.Type == TypeSymlink { // e.g. "/Volumes/Macintosh HD" → "/"
		return true
	}
	vs, err := lstat("/Volumes")
	if err != nil {
		return false
	}
	return vi.Dev != vs.Dev
}

// Under reports whether a is b or inside b.
func Under(a, b string) bool {
	if b == "/" {
		return strings.HasPrefix(a, "/")
	}
	return a == b || strings.HasPrefix(a, b+"/")
}

// StrictlyUnder reports whether a is inside b (and not b itself).
func StrictlyUnder(a, b string) bool {
	return a != b && Under(a, b)
}

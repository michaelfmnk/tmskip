//go:build darwin

package fsys

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"golang.org/x/sys/unix"
)

// OS is the real filesystem.
type OS struct{}

func typeOf(m fs.FileMode) Type {
	switch {
	case m&fs.ModeSymlink != 0:
		return TypeSymlink
	case m.IsDir():
		return TypeDir
	case m.IsRegular():
		return TypeFile
	}
	return TypeOther
}

func (OS) Lstat(p string) (Info, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return Info{}, err
	}
	info := Info{Type: typeOf(fi.Mode()), Mode: fi.Mode().Perm(), Size: fi.Size()}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		info.Blocks = st.Blocks
		info.Dev = uint64(st.Dev)
		info.Ino = st.Ino
		info.Nlink = uint64(st.Nlink)
	}
	return info, nil
}

func (OS) ReadDir(p string) ([]Entry, error) {
	des, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		out = append(out, Entry{Name: de.Name(), Type: typeOf(de.Type())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (OS) Readlink(p string) (string, error) { return os.Readlink(p) }

func xattrErr(op, p string, err error) error {
	if errors.Is(err, unix.ENOATTR) {
		return ErrNoAttr
	}
	return &fs.PathError{Op: op, Path: p, Err: err}
}

// GetXattr reads an xattr of p itself (lgetxattr = XATTR_NOFOLLOW).
func (OS) GetXattr(p, name string) ([]byte, error) {
	buf := make([]byte, 256)
	for {
		n, err := unix.Lgetxattr(p, name, buf)
		if errors.Is(err, unix.ERANGE) {
			size, err2 := unix.Lgetxattr(p, name, nil)
			if err2 != nil {
				return nil, xattrErr("getxattr", p, err2)
			}
			buf = make([]byte, size)
			continue
		}
		if err != nil {
			return nil, xattrErr("getxattr", p, err)
		}
		return buf[:n], nil
	}
}

func (OS) SetXattr(p, name string, value []byte) error {
	if err := unix.Lsetxattr(p, name, value, 0); err != nil {
		return xattrErr("setxattr", p, err)
	}
	return nil
}

func (OS) RemoveXattr(p, name string) error {
	err := unix.Lremovexattr(p, name)
	if err != nil && !errors.Is(err, unix.ENOATTR) {
		return xattrErr("removexattr", p, err)
	}
	return nil
}

func (OS) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

// WriteFileAtomic writes to a temp file in the same directory and renames it
// over p.
func (OS) WriteFileAtomic(p string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(p), "."+filepath.Base(p)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	cleanup := func(err error) error {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if _, err := f.Write(data); err != nil {
		return cleanup(err)
	}
	if err := f.Chmod(perm.Perm()); err != nil {
		return cleanup(err)
	}
	if err := f.Sync(); err != nil {
		return cleanup(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (OS) AppendFile(p string, data []byte) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func (OS) MkdirAll(p string, perm os.FileMode) error { return os.MkdirAll(p, perm) }
func (OS) Rename(oldp, newp string) error            { return os.Rename(oldp, newp) }
func (OS) RemoveAll(p string) error                  { return os.RemoveAll(p) }

func (OS) Probe(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	return f.Close()
}

func (o OS) VolumeMounted(p string) bool { return VolumeMountedVia(o.Lstat, p) }

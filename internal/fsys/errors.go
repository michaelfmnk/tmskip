package fsys

import (
	"errors"
	"io/fs"
	"syscall"
)

// ErrNoAttr is returned by GetXattr when the attribute is absent.
var ErrNoAttr = errors.New("no such xattr")

// IsDenied reports a TCC denial (Full Disk Access): EPERM.
func IsDenied(err error) bool { return errors.Is(err, syscall.EPERM) }

// IsUnreadable reports EPERM or EACCES.
func IsUnreadable(err error) bool {
	return errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES)
}

// IsNotExist reports that the path (or a parent) does not exist.
func IsNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// ErrText is err without the operation and path a *fs.PathError adds, e.g.
// "operation not permitted".
func ErrText(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

//go:build darwin

package state

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// FlockLocker is the real lock: flock(LOCK_EX|LOCK_NB) on a file, held
// until unlock or process exit (the kernel releases it, so no stale locks).
type FlockLocker struct{}

func (FlockLocker) TryLock(path string) (func(), bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, false, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, true, nil
}

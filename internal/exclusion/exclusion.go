// Package exclusion reads and writes the two xattrs tmskip cares about: the
// sticky Time Machine exclusion (as written by `tmutil addexclusion` without
// -p) and tmskip's ownership marker.
package exclusion

import (
	"errors"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

const (
	// AttrName is the sticky Time Machine exclusion xattr.
	AttrName = "com.apple.metadata:com_apple_backup_excludeItem"
	// MarkerName marks folders tmskip excluded ("is this mine?").
	MarkerName = "dev.tmskip.managed"
)

// MarkerValue is the marker's value; its presence is the proof.
var MarkerValue = []byte("1")

// Value is what `tmutil addexclusion` writes: a binary plist of the string
// "com.apple.backupd" ("bplist00" + string + offset table + 32-byte trailer).
// The integration test verifies it against tmutil byte for byte.
var Value = []byte{
	0x62, 0x70, 0x6C, 0x69, 0x73, 0x74, 0x30, 0x30, 0x5F, 0x10, 0x11, 0x63, 0x6F, 0x6D, 0x2E, 0x61,
	0x70, 0x70, 0x6C, 0x65, 0x2E, 0x62, 0x61, 0x63, 0x6B, 0x75, 0x70, 0x64, 0x08, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x1C,
}

// Store is the exclusion/marker state of folders. All calls act on the path
// itself, never through a symlink.
type Store interface {
	IsExcluded(path string) (bool, error)
	IsMarked(path string) (bool, error)
	Mark(path string) error
	Exclude(path string) error
	Include(path string) error
	Unmark(path string) error
}

// New returns the xattr-backed Store.
func New(fs fsys.FS) Store { return xattrStore{fs} }

type xattrStore struct{ fs fsys.FS }

func (s xattrStore) has(p, name string) (bool, error) {
	_, err := s.fs.GetXattr(p, name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fsys.ErrNoAttr) {
		return false, nil
	}
	return false, err
}

func (s xattrStore) IsExcluded(p string) (bool, error) { return s.has(p, AttrName) }
func (s xattrStore) IsMarked(p string) (bool, error)   { return s.has(p, MarkerName) }
func (s xattrStore) Mark(p string) error               { return s.fs.SetXattr(p, MarkerName, MarkerValue) }
func (s xattrStore) Exclude(p string) error            { return s.fs.SetXattr(p, AttrName, Value) }
func (s xattrStore) Include(p string) error            { return s.fs.RemoveXattr(p, AttrName) }
func (s xattrStore) Unmark(p string) error             { return s.fs.RemoveXattr(p, MarkerName) }

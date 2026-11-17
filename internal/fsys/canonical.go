package fsys

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

const maxSymlinkHops = 255

// Canonical resolves p through the filesystem: symlinks followed, each
// component's letter case taken as stored on disk (never lowercased). If a
// component doesn't exist, the canonical existing prefix is returned with the
// rest as typed and exists=false; the same path comes back with the error
// when a component can't be read (EACCES, EPERM). It is pure over Lstat/ReadDir/Readlink so
// the in-memory fake exercises it.
func Canonical(fs FS, p string) (canon string, exists bool, err error) {
	if !strings.HasPrefix(p, "/") {
		return "", false, fmt.Errorf("%q is not an absolute path", p)
	}
	queue := components(path.Clean(p))
	cur := "/"
	hops := 0
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		picked := c
		if entries, err := fs.ReadDir(cur); err == nil {
			picked = pick(entries, c)
		}
		next := path.Join(cur, picked)
		info, err := fs.Lstat(next)
		if err != nil {
			partial := path.Join(append([]string{next}, queue...)...)
			if IsNotExist(err) {
				return partial, false, nil
			}
			// Unreadable (EACCES/EPERM): the canonical prefix plus the typed
			// tail, so callers can still name and guard the path.
			return partial, false, err
		}
		if info.Type == TypeSymlink {
			hops++
			if hops > maxSymlinkHops {
				return "", false, errors.New(p + ": too many symlinks")
			}
			target, err := fs.Readlink(next)
			if err != nil {
				return "", false, err
			}
			if !strings.HasPrefix(target, "/") {
				target = path.Join(cur, target)
			}
			queue = append(components(path.Clean(target)), queue...)
			cur = "/"
			continue
		}
		cur = next
	}
	return cur, true, nil
}

// pick returns the stored name for c: the exact match, else the unique
// case-insensitive match, else c as typed.
func pick(entries []Entry, c string) string {
	var fold []string
	for _, e := range entries {
		if e.Name == c {
			return c
		}
		if strings.EqualFold(e.Name, c) {
			fold = append(fold, e.Name)
		}
	}
	if len(fold) == 1 {
		return fold[0]
	}
	return c
}

func components(clean string) []string {
	if clean == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(clean, "/"), "/")
}

// Exists reports whether p exists, following symlinks (stat, not lstat): a
// dangling symlink does not exist.
func Exists(fs FS, p string) bool {
	_, ok, err := Canonical(fs, p)
	return err == nil && ok
}

package config

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// MissingError: there is no config file at Path. Target is set when Path is
// a symlink whose target doesn't exist. The CLI maps it to exit 2.
type MissingError struct {
	Path   string
	Target string
}

func (e *MissingError) Error() string {
	if e.Target != "" {
		return fmt.Sprintf("config %s is a symlink to %s, which doesn't exist — fix the link, or run \"tmskip init\"", e.Path, e.Target)
	}
	return fmt.Sprintf("no config at %s — run \"tmskip init\"", e.Path)
}

// ReadError: the config exists but can't be read (EACCES, or EPERM when
// it is in a folder macOS protects and there is no Full Disk Access).
type ReadError struct {
	Path string
	Err  error
}

func (e *ReadError) Error() string { return fmt.Sprintf("reading config %s: %v", e.Path, e.Err) }
func (e *ReadError) Unwrap() error { return e.Err }

// Load reads, parses and validates the config at p.
func Load(fs fsys.FS, p string) (*File, error) {
	data, err := fs.ReadFile(p)
	if err != nil {
		if fsys.IsNotExist(err) {
			return nil, missing(fs, p)
		}
		return nil, &ReadError{Path: p, Err: err}
	}
	f, err := Parse(data)
	if err == nil {
		err = f.Validate()
	}
	if err != nil {
		var ie *InvalidError
		if errors.As(err, &ie) {
			ie.Path = p
		}
		return nil, err
	}
	return f, nil
}

// missing builds a MissingError, naming the dangling target when p is a
// symlink (followed through any chain of links).
func missing(fs fsys.FS, p string) *MissingError {
	e := &MissingError{Path: p}
	cur := p
	for i := 0; i < 32; i++ {
		info, err := fs.Lstat(cur)
		if err != nil || info.Type != fsys.TypeSymlink {
			break
		}
		t, err := fs.Readlink(cur)
		if err != nil {
			break
		}
		if !strings.HasPrefix(t, "/") {
			t = path.Join(path.Dir(cur), t)
		}
		e.Target = t
		cur = t
	}
	return e
}

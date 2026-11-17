package config

import (
	"bytes"
	"fmt"
	"os"
	"path"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// AppendGroups appends the groups' verbatim text to a config, preserving
// everything already there (comments, order) byte for byte. It refuses —
// all or nothing — if any group's name is already a rule, and validates the
// result.
func AppendGroups(current []byte, groups []Group) ([]byte, error) {
	f, err := Parse(current)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, r := range f.Rules {
		have[r.Name] = true
	}
	for _, g := range groups {
		if have[g.Name] {
			return nil, fmt.Errorf("rule %q already exists — nothing added", g.Name)
		}
	}
	var b bytes.Buffer
	b.Write(current)
	if len(current) > 0 && !bytes.HasSuffix(current, []byte("\n")) {
		b.WriteByte('\n')
	}
	for _, g := range groups {
		b.WriteByte('\n')
		b.WriteString(g.Text)
	}
	out := b.Bytes()
	nf, err := Parse(out)
	if err == nil {
		err = nf.Validate()
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Target is the file a config path really is: symlinks followed.
func Target(fs fsys.FS, p string) (string, error) {
	t, _, err := fsys.Canonical(fs, p)
	return t, err
}

// WriteThrough writes data to the config at p, through any symlink into its
// target (the link stays a link), atomically, keeping the file's mode.
func WriteThrough(fs fsys.FS, p string, data []byte) error {
	target, err := Target(fs, p)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := fs.Lstat(target); err == nil {
		mode = info.Mode.Perm()
	}
	if err := fs.MkdirAll(path.Dir(target), 0o755); err != nil {
		return err
	}
	return fs.WriteFileAtomic(target, data, mode)
}

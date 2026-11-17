package cli

import (
	"path/filepath"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

var rootCandidates = []string{
	"Developer", "Projects", "projects", "src", "code", "dev", "work", "repos", "git",
	// JetBrains and other IDE defaults
	"IdeaProjects", "PycharmProjects", "WebstormProjects", "GolandProjects", "CLionProjects",
	"PhpstormProjects", "RubymineProjects", "RiderProjects", "RustRoverProjects",
	"DataspellProjects", "AndroidStudioProjects", "eclipse-workspace",
}

// detectRoots lists the usual code folders that exist under $HOME, deduped
// canonically (~/projects and ~/Projects are one folder on a
// case-insensitive volume; a symlink to another candidate is that one),
// written as ~/<name> with the case stored on disk.
func detectRoots(env sysenv.Env, fs fsys.FS) []string {
	home, _, err := fsys.Canonical(fs, env.Home)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, c := range rootCandidates {
		canon, ok, err := fsys.Canonical(fs, filepath.Join(env.Home, c))
		if err != nil || !ok || seen[canon] {
			continue
		}
		if info, err := fs.Lstat(canon); err != nil || info.Type != fsys.TypeDir {
			continue
		}
		seen[canon] = true
		name := "~/" + c
		if rel := strings.TrimPrefix(canon, home+"/"); rel != canon {
			name = "~/" + rel
		}
		out = append(out, name)
	}
	return out
}

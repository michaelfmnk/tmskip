package cli

import (
	"path/filepath"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// protectedDirs are the TCC-protected locations that need Full Disk Access
// to be read by a background process.
func protectedDirs(home string) []string {
	out := []string{}
	for _, d := range []string{"Desktop", "Documents", "Downloads", "Library/Mobile Documents", "Library/CloudStorage"} {
		out = append(out, filepath.Join(home, d))
	}
	return append(out, "/Volumes")
}

// needsFDA lists the roots and fixed paths in (or containing) a
// TCC-protected location, in config order: roots, then fixed paths.
func needsFDA(r *config.Resolved) []string {
	prot := protectedDirs(r.Home)
	hit := func(p string, containing bool) bool {
		for _, d := range prot {
			if fsys.StrictlyUnder(p, d) || p == d && d != "/Volumes" || containing && fsys.StrictlyUnder(d, p) {
				return true
			}
		}
		return false
	}
	var out []string
	for _, root := range r.Roots {
		if hit(root, true) {
			out = append(out, root)
		}
	}
	for _, rule := range r.Rules {
		for _, f := range rule.Fixed {
			if hit(f, false) {
				out = append(out, f)
			}
		}
	}
	return out
}

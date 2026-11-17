package engine

import (
	"path"
	"sort"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// Sweep plans the uninstall: every folder carrying tmskip's marker —
// found by walking the roots (stopping at marked folders), the fixed paths
// and the hint list — is re-included. r may be nil (broken config): then
// only the hints are swept. Unreadable or unmounted ones are left
// untouched and reported.
func Sweep(r *config.Resolved, fs fsys.FS, ex exclusion.Store, hints []state.Owned) *Plan {
	plan := &Plan{}
	hints = CanonicalHints(fs, hints)
	candidates := map[string]bool{}
	isHint := map[string]bool{}
	for _, h := range hints {
		candidates[h.Path] = true
		isHint[h.Path] = true
	}
	if r != nil {
		var visit func(dir string)
		visit = func(dir string) {
			listing, err := fs.ReadDir(dir)
			if err != nil {
				if !fsys.IsNotExist(err) {
					plan.Problems = append(plan.Problems, newProblem(dir, err))
				}
				return
			}
			for _, e := range listing {
				if e.Type != fsys.TypeDir || r.Skip[e.Name] {
					continue
				}
				child := path.Join(dir, e.Name)
				marked, err := ex.IsMarked(child)
				switch {
				case err != nil:
					candidates[child] = true // reported below
				case marked:
					candidates[child] = true // not descended: nested ones come from hints
				default:
					visit(child)
				}
			}
		}
		for _, root := range r.Roots {
			if marked, err := ex.IsMarked(root); err == nil && marked {
				candidates[root] = true
			}
			visit(root)
		}
		for _, rule := range r.Rules {
			for _, f := range rule.Fixed {
				candidates[f] = true
			}
		}
	}

	paths := make([]string, 0, len(candidates))
	for p := range candidates {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	untouched := func(p, note string) {
		plan.Actions = append(plan.Actions, Action{Op: OpUntouched, Path: p, Note: note})
		if isHint[p] {
			plan.Hints = append(plan.Hints, state.Owned{Path: p})
		}
	}
	for _, p := range paths {
		if _, err := fs.Lstat(p); err != nil {
			switch {
			case fsys.IsNotExist(err) && !fs.VolumeMounted(p):
				untouched(p, "volume not mounted")
			case fsys.IsNotExist(err):
				// gone: nothing to re-include
			default:
				plan.Problems = append(plan.Problems, newProblem(p, err))
				untouched(p, "unreadable: "+fsys.ErrText(err))
			}
			continue
		}
		marked, err := ex.IsMarked(p)
		if err != nil {
			plan.Problems = append(plan.Problems, newProblem(p, err))
			untouched(p, "unreadable: "+fsys.ErrText(err))
			continue
		}
		if marked {
			plan.Actions = append(plan.Actions, Action{Op: OpInclude, Path: p, Note: "uninstall"})
		}
	}
	return plan
}

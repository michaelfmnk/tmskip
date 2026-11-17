package engine

import (
	"errors"
	"sort"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// Apply carries out a plan's + and - actions: Mark then Exclude; Include
// then Unmark. It continues past per-path errors. r may be nil (uninstall);
// otherwise every exclusion re-checks the guardrails first. The returned
// hints are the plan's, corrected for what actually happened: a path keeps
// its hint as long as it still carries the marker.
func Apply(r *config.Resolved, fs fsys.FS, ex exclusion.Store, p *Plan) (applied []Action, hints []state.Owned, problems []Problem) {
	hintSet := map[string]state.Owned{}
	for _, h := range p.Hints {
		hintSet[h.Path] = h
	}
	// fail reports a per-path error; a path that vanished since the walk
	// (a build deleted it) is no problem and loses its hint.
	fail := func(path string, err error) bool {
		if fsys.IsNotExist(err) {
			delete(hintSet, path)
			return false
		}
		problems = append(problems, newProblem(path, err))
		return true
	}

	for _, a := range p.Actions {
		switch a.Op {
		case OpExclude:
			if c, _, err := fsys.Canonical(fs, a.Path); err != nil || c != a.Path {
				// Guardrails compare canonical paths: never act on another spelling.
				fail(a.Path, errors.New("refusing to exclude: not a canonical path"))
				delete(hintSet, a.Path)
				continue
			}
			if r != nil {
				if reason, blocked := Blocked(r, a.Path); blocked {
					fail(a.Path, errors.New("refusing to exclude: "+reason))
					delete(hintSet, a.Path)
					continue
				}
			}
			if err := ex.Mark(a.Path); err != nil {
				fail(a.Path, err)
				delete(hintSet, a.Path)
				continue
			}
			if err := ex.Exclude(a.Path); err != nil {
				fail(a.Path, err) // marked, not excluded: the next sync repairs it
				continue
			}
			applied = append(applied, a)
		case OpInclude:
			if err := ex.Include(a.Path); err != nil {
				if fail(a.Path, err) {
					hintSet[a.Path] = state.Owned{Path: a.Path}
				}
				continue
			}
			if err := ex.Unmark(a.Path); err != nil {
				if fail(a.Path, err) {
					hintSet[a.Path] = state.Owned{Path: a.Path}
				}
				continue
			}
			applied = append(applied, a)
		}
	}
	for _, h := range hintSet {
		hints = append(hints, h)
	}
	sort.Slice(hints, func(i, j int) bool { return hints[i].Path < hints[j].Path })
	return applied, hints, problems
}

package engine

import (
	"path"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// ScanResult is what a walk of the roots and fixed paths found.
type ScanResult struct {
	Desired    map[string]Decision // to be excluded, keyed by canonical path
	Blocked    []Decision          // matched but guardrail-blocked (walked into)
	MarkedSeen []string            // visited paths that carry the marker (roots included)
	Problems   []Problem
}

func newProblem(p string, err error) Problem {
	return Problem{Path: p, Err: err, Denied: fsys.IsDenied(err)}
}

func entryNames(es []fsys.Entry) map[string]bool {
	m := make(map[string]bool, len(es))
	for _, e := range es {
		m[e.Name] = true
	}
	return m
}

// Scan walks every root depth-first in name order, never following symlinks
// or descending into skipped or matched folders, then adds the fixed paths.
func Scan(r *config.Resolved, fs fsys.FS, ex exclusion.Store) *ScanResult {
	return ScanWith(r, fs, ex, nil)
}

// ScanWith is Scan reporting each listed folder to p (which may be nil).
func ScanWith(r *config.Resolved, fs fsys.FS, ex exclusion.Store, p Progress) *ScanResult {
	s := &scanner{r: r, fs: fs, ex: ex, res: &ScanResult{Desired: map[string]Decision{}}, decided: map[string]bool{}, progress: p}
	// Configured paths that couldn't be resolved are read problems, and
	// then nothing is walked: a symlink behind the unreadable folder may
	// point anywhere, so no decision would be safe (see Reconcile).
	for _, u := range r.Unreadable {
		s.res.Problems = append(s.res.Problems, newProblem(u.Path, u.Err))
	}
	if len(r.Unreadable) > 0 {
		return s.res
	}
	for _, root := range r.Roots {
		listing, err := fs.ReadDir(root)
		if err != nil {
			if !fsys.IsNotExist(err) && !s.reported(root) { // a missing root is only a config warning
				s.res.Problems = append(s.res.Problems, newProblem(root, err))
			}
			continue
		}
		s.listed(root)
		s.checkMarker(root)
		s.visit(root, listing)
	}
	for _, rule := range r.Rules {
		for _, f := range rule.Fixed {
			if s.decided[f] {
				continue
			}
			if _, err := fs.Lstat(f); err != nil {
				if !fsys.IsNotExist(err) {
					s.res.Problems = append(s.res.Problems, newProblem(f, err))
				}
				continue
			}
			if !s.checkMarker(f) {
				continue
			}
			s.decide(f, Match{Rule: rule.Name, Key: "paths"}, ReasonFixed)
		}
	}
	return s.res
}

type scanner struct {
	r   *config.Resolved
	fs  fsys.FS
	ex  exclusion.Store
	res *ScanResult
	// decided holds paths already matched (desired or blocked), so a fixed
	// path met by the walk isn't decided twice.
	decided map[string]bool

	progress Progress
	folders  int // folders listed so far
}

func (s *scanner) listed(dir string) {
	s.folders++
	if s.progress != nil {
		s.progress.Scanning(dir, s.folders, len(s.res.Desired))
	}
}

// reported: a problem for p is already listed (e.g. from the config).
func (s *scanner) reported(p string) bool {
	for _, pr := range s.res.Problems {
		if pr.Path == p {
			return true
		}
	}
	return false
}

// checkMarker records a marked path; false means the path vanished or its
// marker couldn't be read (reported).
func (s *scanner) checkMarker(p string) bool {
	marked, err := s.ex.IsMarked(p)
	if err != nil {
		if !fsys.IsNotExist(err) {
			s.res.Problems = append(s.res.Problems, newProblem(p, err))
		}
		return false
	}
	if marked {
		s.res.MarkedSeen = append(s.res.MarkedSeen, p)
	}
	return true
}

// decide records a match: Desired unless a guardrail blocks it. It reports
// whether the path is desired (and so must not be descended).
func (s *scanner) decide(p string, m Match, reason Reason) bool {
	s.decided[p] = true
	if guard, blocked := Blocked(s.r, p); blocked {
		s.res.Blocked = append(s.res.Blocked, Decision{Path: p, Reason: ReasonBlocked, Match: m, Guard: guard})
		return false
	}
	s.res.Desired[p] = Decision{Path: p, Exclude: true, Reason: reason, Match: m}
	return true
}

func (s *scanner) visit(dir string, listing []fsys.Entry) {
	siblings := entryNames(listing)
	for _, e := range listing {
		if e.Type != fsys.TypeDir || s.r.Skip[e.Name] {
			continue
		}
		child := path.Join(dir, e.Name)
		childListing, err := s.fs.ReadDir(child)
		if err != nil {
			if !fsys.IsNotExist(err) { // vanished mid-walk: nothing to do
				s.res.Problems = append(s.res.Problems, newProblem(child, err))
			}
			continue
		}
		s.listed(child)
		if !s.checkMarker(child) {
			continue
		}
		var m Match
		var ok bool
		reason := ReasonMatched
		if rule, fixed := s.r.IsFixed(child); fixed {
			m, ok, reason = Match{Rule: rule, Key: "paths"}, true, ReasonFixed
		} else {
			m, ok = MatchDir(s.r, e.Name, entryNames(childListing), siblings)
		}
		if ok && s.decide(child, m, reason) {
			continue
		}
		s.visit(child, childListing)
	}
}

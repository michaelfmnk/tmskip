package engine

import (
	"path"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// Evaluate decides one canonical path without a walk. It reads no xattrs.
// Used for hint-list entries the walk didn't reach, and by why/list.
// withCoveredBy also finds the topmost excluded ancestor below the root.
func Evaluate(r *config.Resolved, fs fsys.FS, p string, withCoveredBy bool) Decision {
	d := Decision{Path: p}
	info, err := fs.Lstat(p)
	if err != nil {
		switch {
		case fsys.IsNotExist(err) && !fs.VolumeMounted(p):
			d.Reason = ReasonUnmounted
		case fsys.IsNotExist(err):
			d.Reason = ReasonMissing
		default:
			d.Reason, d.Err = ReasonUnreadable, err
		}
		return d
	}
	if rule, fixed := r.IsFixed(p); fixed {
		d.Match = Match{Rule: rule, Key: "paths"}
		return guarded(r, d, ReasonFixed)
	}
	if guard, blocked := rootGuard(r, p); blocked {
		d.Reason, d.Guard = ReasonBlocked, guard
		return d
	}
	root := ""
	for _, rt := range r.Roots {
		if fsys.StrictlyUnder(p, rt) {
			root = rt
			break
		}
	}
	if root == "" {
		d.Reason = ReasonOutsideRoots
		return d
	}
	for _, c := range strings.Split(strings.TrimPrefix(p, root+"/"), "/") {
		if r.Skip[c] {
			d.Reason = ReasonSkipped
			return d
		}
	}
	if info.Type != fsys.TypeDir {
		d.Reason = ReasonNotMatched
		return d
	}
	listing, err := fs.ReadDir(p)
	if err != nil {
		return gone(fs, d, err)
	}
	siblings, err := fs.ReadDir(path.Dir(p))
	if err != nil {
		return gone(fs, d, err)
	}
	m, ok := MatchDir(r, path.Base(p), entryNames(listing), entryNames(siblings))
	if !ok {
		d.Reason = ReasonNotMatched
	} else {
		d.Match = m
		d = guarded(r, d, ReasonMatched)
	}
	if withCoveredBy {
		d.CoveredBy = coveredBy(r, fs, root, p)
	}
	return d
}

// gone classifies a listing error: a folder that vanished after Lstat (a
// build deleting it) is Missing/Unmounted, not unreadable.
func gone(fs fsys.FS, d Decision, err error) Decision {
	switch {
	case fsys.IsNotExist(err) && !fs.VolumeMounted(d.Path):
		d.Reason = ReasonUnmounted
	case fsys.IsNotExist(err):
		d.Reason = ReasonMissing
	default:
		d.Reason, d.Err = ReasonUnreadable, err
	}
	return d
}

// guarded marks d as reason, or Blocked if a guardrail applies.
func guarded(r *config.Resolved, d Decision, reason Reason) Decision {
	if guard, blocked := Blocked(r, d.Path); blocked {
		d.Reason, d.Guard = ReasonBlocked, guard
		return d
	}
	d.Reason, d.Exclude = reason, true
	return d
}

// coveredBy returns the topmost ancestor of p strictly below root that is
// itself excluded (the walk stops there, so p is inside its exclusion).
func coveredBy(r *config.Resolved, fs fsys.FS, root, p string) string {
	rel := strings.Split(strings.TrimPrefix(p, root+"/"), "/")
	cur := root
	for _, c := range rel[:len(rel)-1] {
		cur = cur + "/" + c
		if a := Evaluate(r, fs, cur, false); a.Exclude {
			return cur
		}
	}
	return ""
}

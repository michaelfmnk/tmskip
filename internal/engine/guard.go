package engine

import (
	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// Blocked applies the guardrails (spec: in code, not config). p must be
// canonical. A blocked path is never excluded.
func Blocked(r *config.Resolved, p string) (reason string, blocked bool) {
	if reason, blocked := rootGuard(r, p); blocked {
		return reason, true
	}
	for _, k := range r.Keep {
		if fsys.Under(p, k) {
			return "is kept by " + k, true
		}
	}
	for _, k := range r.Keep {
		if fsys.StrictlyUnder(k, p) {
			return "contains kept path " + k, true
		}
	}
	if _, fixed := r.IsFixed(p); fixed {
		return "", false
	}
	for _, root := range r.Roots {
		if fsys.StrictlyUnder(p, root) {
			return "", false
		}
	}
	return "is outside roots and fixed paths", true
}

// rootGuard is the guardrails about $HOME and the scan roots: $HOME, a
// scan root, an ancestor of a scan root, an ancestor of $HOME. They hold
// whatever rule (or none) applies.
func rootGuard(r *config.Resolved, p string) (reason string, blocked bool) {
	if p == r.Home {
		return "is your home folder", true
	}
	for _, root := range r.Roots {
		if p == root {
			return "is a scan root", true
		}
	}
	for _, root := range r.Roots {
		if fsys.StrictlyUnder(root, p) {
			return "contains scan root " + root, true
		}
	}
	// Exclusions are recursive: excluding an ancestor of $HOME excludes $HOME.
	if fsys.StrictlyUnder(r.Home, p) {
		return "contains your home folder", true
	}
	return "", false
}

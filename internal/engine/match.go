// Package engine decides which folders tmskip excludes: rule matching,
// guardrails, the walk, single-path evaluation, reconcile, apply, the
// uninstall sweep and size measurement. It touches the disk only through
// fsys.FS and exclusion.Store.
package engine

import (
	"sort"

	"github.com/michaelfmnk/tmskip/internal/config"
)

// Match says which rule matched a folder and why. Key is "names",
// "contains", "parent_has" or "paths"; Gate is the entry name that decided
// it (for `why`).
type Match struct {
	Rule string
	Key  string
	Gate string
}

// MatchDir matches a folder called name, whose direct entries are children
// and whose parent's entries are siblings, against the enabled match rules.
// Keys are AND-ed, values OR-ed; the first rule in config order wins.
func MatchDir(r *config.Resolved, name string, children, siblings map[string]bool) (Match, bool) {
	for _, rule := range r.Rules {
		if !rule.IsMatch() {
			continue
		}
		m := Match{Rule: rule.Name}
		if len(rule.Names) > 0 {
			if !rule.Names[name] {
				continue
			}
			m.Key, m.Gate = "names", name
		}
		if len(rule.Contains) > 0 {
			g, ok := firstIn(children, rule.Contains)
			if !ok {
				continue
			}
			m.Key, m.Gate = "contains", g
		}
		if len(rule.ParentHas) > 0 {
			g, ok := firstIn(siblings, rule.ParentHas)
			if !ok {
				continue
			}
			m.Key, m.Gate = "parent_has", g
		}
		if m.Key == "" {
			continue
		}
		return m, true
	}
	return Match{}, false
}

// firstIn returns the alphabetically first name in entries that is in want.
func firstIn(entries, want map[string]bool) (string, bool) {
	var hits []string
	for w := range want {
		if entries[w] {
			hits = append(hits, w)
		}
	}
	if len(hits) == 0 {
		return "", false
	}
	sort.Strings(hits)
	return hits[0], true
}

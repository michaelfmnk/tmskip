package config

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// Resolved is the config compiled against the filesystem: every path
// canonical, disabled rules dropped.
type Resolved struct {
	Path     string // config path as resolved (not symlink-resolved)
	Home     string // canonical $HOME
	Roots    []string
	Skip     map[string]bool
	Keep     []string
	Notify   bool
	Interval time.Duration // 0 if [schedule] is absent
	Rules    []CompiledRule
	Notes    []string // shown under -v: deduped roots, missing fixed paths
	// Unreadable are configured paths with a component that can't be read
	// (no search permission, or TCC): read problems, reported by the scan.
	Unreadable []Unreadable
}

// Unreadable is a configured path that couldn't be resolved because a
// component can't be read. Path is the canonical prefix plus the typed tail.
type Unreadable struct {
	Path string
	Err  error
}

// CompiledRule is an enabled rule with canonical fixed paths and name sets.
type CompiledRule struct {
	Name                       string
	Fixed                      []string // canonical, existing
	Names, Contains, ParentHas map[string]bool
}

// IsMatch reports whether the rule is a match rule (not fixed paths).
func (c CompiledRule) IsMatch() bool { return c.Fixed == nil }

// Warning is a config problem that doesn't stop a run (shown by sync and
// doctor, even under -q).
type Warning struct{ Text string }

// Compile expands ~ and canonicalises every configured path.
func Compile(f *File, path string, env sysenv.Env, fs fsys.FS) (*Resolved, []Warning, error) {
	if env.Home == "" {
		return nil, nil, errors.New("HOME is not set")
	}
	home, _, err := fsys.Canonical(fs, env.Home)
	if err != nil {
		return nil, nil, fmt.Errorf("HOME: %w", err)
	}
	r := &Resolved{Path: path, Home: home, Skip: map[string]bool{}, Notify: f.Notify}
	var warnings []Warning
	var problems []string
	// canon resolves a configured path. unreadable: a component can't be
	// read; the path (canonical prefix + typed tail) is still returned and
	// recorded as a read problem with a warning — never an invalid config.
	canon := func(label, display, typed string) (c string, ok, unreadable bool) {
		c, ok, err := fsys.Canonical(fs, ExpandHome(typed, env.Home))
		if err != nil {
			if fsys.IsUnreadable(err) && c != "" {
				warnings = append(warnings, Warning{fmt.Sprintf("%s can't be read: %s", display, fsys.ErrText(err))})
				r.Unreadable = append(r.Unreadable, Unreadable{Path: c, Err: err})
				return c, false, true
			}
			problems = append(problems, fmt.Sprintf("%s: %q: %v", label, typed, err))
			return "", false, false
		}
		return c, ok, false
	}

	type root struct{ canon, typed string }
	var roots []root
	for _, typed := range f.Roots {
		c, ok, unreadable := canon("roots", "root "+typed, typed)
		if c == "" {
			continue
		}
		if !ok && !unreadable {
			warnings = append(warnings, Warning{"root " + typed + " does not exist"})
		}
		roots = append(roots, root{c, typed})
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].canon < roots[j].canon })
	var kept []root
	for _, rt := range roots {
		dup := false
		for _, k := range kept {
			if rt.canon == k.canon {
				r.Notes = append(r.Notes, fmt.Sprintf("root %s is the same folder as %s — ignored", rt.typed, k.typed))
				dup = true
				break
			}
			if fsys.Under(rt.canon, k.canon) {
				r.Notes = append(r.Notes, fmt.Sprintf("root %s is inside %s — ignored", rt.typed, k.typed))
				dup = true
				break
			}
		}
		if !dup {
			kept = append(kept, rt)
			r.Roots = append(r.Roots, rt.canon)
		}
	}

	for _, typed := range f.Keep {
		c, ok, unreadable := canon("keep", "keep "+typed, typed)
		if c == "" {
			continue
		}
		if !ok && !unreadable {
			warnings = append(warnings, Warning{"keep " + typed + " does not exist"})
		}
		r.Keep = append(r.Keep, c)
	}
	for _, s := range f.Skip {
		r.Skip[s] = true
	}
	if f.Schedule != nil {
		if d, err := ParseInterval(f.Schedule.Interval); err == nil {
			r.Interval = d
		}
	}

	for _, rule := range f.Rules {
		if !rule.IsEnabled() {
			continue
		}
		cr := CompiledRule{Name: rule.Name, Names: set(rule.Names), Contains: set(rule.Contains), ParentHas: set(rule.ParentHas)}
		if rule.Paths != nil {
			cr.Fixed = []string{}
			for _, typed := range rule.Paths {
				c, ok, unreadable := canon(fmt.Sprintf("rule %q paths", rule.Name), fmt.Sprintf("fixed path %s (%s)", typed, rule.Name), typed)
				if c == "" || unreadable {
					continue
				}
				if !ok {
					r.Notes = append(r.Notes, fmt.Sprintf("fixed path %s (%s) does not exist — skipped", typed, rule.Name))
					continue
				}
				cr.Fixed = append(cr.Fixed, c)
			}
		}
		r.Rules = append(r.Rules, cr)
	}
	if len(problems) > 0 {
		return nil, nil, &InvalidError{Path: path, Problems: problems}
	}
	return r, warnings, nil
}

func set(vals []string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[v] = true
	}
	return m
}

// IsFixed reports whether p is one of an enabled rule's fixed paths.
func (r *Resolved) IsFixed(p string) (rule string, ok bool) {
	for _, cr := range r.Rules {
		for _, f := range cr.Fixed {
			if f == p {
				return cr.Name, true
			}
		}
	}
	return "", false
}

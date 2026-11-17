package config

import (
	"fmt"
	"strings"
	"time"
)

// MinInterval is the shortest schedule interval accepted.
const MinInterval = 5 * time.Minute

// Validate checks everything that can be checked without the filesystem.
// All problems are collected, one per line.
func (f *File) Validate() error {
	var p []string
	add := func(format string, args ...any) { p = append(p, fmt.Sprintf(format, args...)) }

	switch {
	case f.Version == nil:
		add(`missing "version" — add version = 1`)
	case *f.Version != 1:
		add("unsupported version %d — upgrade tmskip", *f.Version)
	}

	checkPaths := func(label string, paths []string) {
		for _, v := range paths {
			if msg := pathProblem(v); msg != "" {
				add("%s: %s", label, msg)
			}
		}
	}
	checkPaths("roots", f.Roots)
	checkPaths("keep", f.Keep)
	for _, v := range f.Skip {
		if msg := nameProblem(v); msg != "" {
			add("skip: %s", msg)
		}
	}

	if f.Schedule != nil {
		if _, err := ParseInterval(f.Schedule.Interval); err != nil {
			add("%s", err)
		}
	}

	names := map[string]bool{}
	for i, r := range f.Rules {
		if r.Name == "" {
			add("rule #%d has no name", i+1)
		} else if names[r.Name] {
			add("duplicate rule name %q", r.Name)
		}
		names[r.Name] = true
		label := fmt.Sprintf("rule %q", r.Name)
		if r.Name == "" {
			label = fmt.Sprintf("rule #%d", i+1)
		}
		hasMatch := r.Names != nil || r.Contains != nil || r.ParentHas != nil
		switch {
		case r.Paths != nil && hasMatch:
			add("%s: paths can't be combined with names/contains/parent_has", label)
		case r.ParentHas != nil && r.Names == nil && r.Contains == nil:
			add("%s: parent_has needs names or contains (it would exclude every sibling of the sentinel)", label)
		case r.Paths == nil && !hasMatch:
			add("%s: needs paths, or names/contains", label)
		}
		for _, kv := range []struct {
			key  string
			vals []string
		}{{"paths", r.Paths}, {"names", r.Names}, {"contains", r.Contains}, {"parent_has", r.ParentHas}} {
			if kv.vals != nil && len(kv.vals) == 0 {
				add("%s: %s is empty", label, kv.key)
			}
		}
		checkPaths(label+" paths", r.Paths)
		for _, list := range [][]string{r.Names, r.Contains, r.ParentHas} {
			for _, v := range list {
				if msg := nameProblem(v); msg != "" {
					add("%s: %s", label, msg)
				}
			}
		}
	}
	if len(p) > 0 {
		return &InvalidError{Problems: p}
	}
	return nil
}

// pathProblem checks a configured path before ~ expansion.
func pathProblem(v string) string {
	switch {
	case v == "~" || strings.HasPrefix(v, "~/"):
		return ""
	case strings.HasPrefix(v, "~"):
		return fmt.Sprintf("%q: only ~ and ~/… are expanded", v)
	case !strings.HasPrefix(v, "/"):
		return fmt.Sprintf("%q is relative — use an absolute path or ~/…", v)
	}
	return ""
}

// nameProblem checks an exact folder/file name.
func nameProblem(v string) string {
	switch {
	case v == "" || v == "." || v == ".." || strings.Contains(v, "/"):
		return fmt.Sprintf("%q is not a name (names are exact folder/file names)", v)
	case strings.ContainsAny(v, "*?["):
		return fmt.Sprintf("globs aren't supported (%q)", v)
	}
	return ""
}

// ParseInterval parses [schedule] interval: a Go duration of whole seconds,
// at least MinInterval.
func ParseInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil || d < MinInterval || d%time.Second != 0 {
		return 0, fmt.Errorf("schedule.interval %q: must be a duration ≥ 5m like \"1h\"", s)
	}
	return d, nil
}

package config

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
)

// File is the config file as written.
type File struct {
	Version  *int      `toml:"version"`
	Roots    []string  `toml:"roots"`
	Skip     []string  `toml:"skip"`
	Keep     []string  `toml:"keep"`
	Notify   bool      `toml:"notify"` // absent → false; init writes true
	Schedule *Schedule `toml:"schedule"`
	Rules    []Rule    `toml:"rule"`
}

// Schedule is the [schedule] table.
type Schedule struct {
	Interval string `toml:"interval"`
}

// Rule is one [[rule]] block.
type Rule struct {
	Name      string   `toml:"name"`
	Enabled   *bool    `toml:"enabled"`
	Paths     []string `toml:"paths"`
	Names     []string `toml:"names"`
	Contains  []string `toml:"contains"`
	ParentHas []string `toml:"parent_has"`
}

// IsEnabled reports whether the rule applies (enabled is absent or true).
func (r Rule) IsEnabled() bool { return r.Enabled == nil || *r.Enabled }

// InvalidError is an invalid config: syntax, unknown keys or validation.
// The CLI maps it to exit 2.
type InvalidError struct {
	Path     string // set by Load
	Problems []string
}

func (e *InvalidError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = "config: " + p
	}
	if e.Path != "" {
		return "invalid config " + e.Path + "\n" + strings.Join(lines, "\n")
	}
	return strings.Join(lines, "\n")
}

// Parse decodes TOML strictly: unknown keys are errors.
func Parse(data []byte) (*File, error) {
	var f File
	md, err := toml.Decode(string(data), &f)
	if err != nil {
		return nil, &InvalidError{Problems: []string{err.Error()}}
	}
	undecoded := md.Undecoded()
	if len(undecoded) == 0 {
		return &f, nil
	}
	// A generic decode tells us which [[rule]] an unknown key sits in.
	var raw map[string]any
	_, _ = toml.Decode(string(data), &raw)
	var problems []string
	seen := map[string]bool{}
	for _, k := range undecoded {
		key := k.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		where := ""
		if len(k) == 2 && k[0] == "rule" {
			where = ruleLocations(raw, k[1])
		}
		problems = append(problems, fmt.Sprintf("unknown key %q%s — check spelling", key, where))
	}
	return nil, &InvalidError{Problems: problems}
}

// ruleLocations names the rules (#N "name") that contain key.
func ruleLocations(raw map[string]any, key string) string {
	rules, _ := raw["rule"].([]map[string]any)
	var locs []string
	for i, r := range rules {
		if _, ok := r[key]; ok {
			name, _ := r["name"].(string)
			locs = append(locs, fmt.Sprintf("rule #%d %q", i+1, name))
		}
	}
	if len(locs) == 0 {
		return ""
	}
	return " (" + strings.Join(locs, ", ") + ")"
}

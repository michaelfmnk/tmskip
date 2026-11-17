package config

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
)

//go:embed defaults.toml
var defaultsTOML string

// Group is one shipped default rule group: its rule and its verbatim text
// (leading comment lines included), which `config add` appends.
type Group struct {
	Name string
	Text string
	Rule Rule
}

var groups = mustParseGroups(defaultsTOML)

// Groups returns the shipped groups in file order.
func Groups() []Group { return slices.Clone(groups) }

// FindGroup returns the shipped group called name.
func FindGroup(name string) (Group, bool) {
	for _, g := range groups {
		if g.Name == name {
			return g, true
		}
	}
	return Group{}, false
}

// GroupNames lists the shipped group names in order.
func GroupNames() []string {
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = g.Name
	}
	return out
}

// mustParseGroups splits the embedded file into blocks: each [[rule]] with
// the comment lines directly above it.
func mustParseGroups(src string) []Group {
	lines := strings.SplitAfter(src, "\n")
	var blocks []string
	start := 0
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			continue
		}
		if b := strings.Join(lines[start:i], ""); strings.TrimSpace(b) != "" {
			blocks = append(blocks, b)
		}
		start = i + 1
	}
	if b := strings.Join(lines[start:], ""); strings.TrimSpace(b) != "" {
		blocks = append(blocks, b)
	}
	var out []Group
	for _, b := range blocks {
		text := strings.TrimRight(b, "\n") + "\n"
		f, err := Parse([]byte("version = 1\n" + text))
		if err != nil || len(f.Rules) != 1 {
			panic(fmt.Sprintf("defaults.toml: bad block %q: %v", text, err))
		}
		out = append(out, Group{Name: f.Rules[0].Name, Text: text, Rule: f.Rules[0]})
	}
	return out
}

// Template is a complete default config with the given roots (as they
// should be written, e.g. "~/Developer") and every shipped group.
func Template(roots []string) string {
	var b strings.Builder
	b.WriteString(`# tmskip config. Every rule tmskip applies is in this file.
# Matched folders get a sticky Time Machine exclusion; preview with "tmskip sync -n".
version = 1

# Folders scanned by the match rules (names/contains/parent_has).
`)
	if len(roots) == 0 {
		b.WriteString("roots = []  # add folders to scan, e.g. [\"~/Developer\"]\n")
	} else {
		quoted := make([]string, len(roots))
		for i, r := range roots {
			quoted[i] = QuoteTOML(r)
		}
		b.WriteString("roots = [" + strings.Join(quoted, ", ") + "]\n")
	}
	b.WriteString(`
# Folder names never descended into or matched.
skip = [".git"]

# Paths never excluded (whatever is inside them stays in backups).
keep = []

# Post a macOS notification when a scheduled run hits a new problem.
notify = true

[schedule]
interval = "1h"
`)
	for _, g := range groups {
		b.WriteString("\n" + g.Text)
	}
	return b.String()
}

// QuoteTOML returns s as a TOML basic string.
func QuoteTOML(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// GroupStatus compares a shipped group with the config by rule name:
// "missing", "present" (same paths/names/contains/parent_has as sets) or
// "changed".
func GroupStatus(g Group, f *File) string {
	for _, r := range f.Rules {
		if r.Name != g.Name {
			continue
		}
		if sameSet(r.Paths, g.Rule.Paths) && sameSet(r.Names, g.Rule.Names) &&
			sameSet(r.Contains, g.Rule.Contains) && sameSet(r.ParentHas, g.Rule.ParentHas) {
			return "present"
		}
		return "changed"
	}
	return "missing"
}

func sameSet(a, b []string) bool {
	as, bs := map[string]bool{}, map[string]bool{}
	for _, v := range a {
		as[v] = true
	}
	for _, v := range b {
		bs[v] = true
	}
	if len(as) != len(bs) {
		return false
	}
	for v := range as {
		if !bs[v] {
			return false
		}
	}
	return true
}

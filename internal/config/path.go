// Package config resolves, parses, validates and compiles tmskip's TOML
// config, and holds the shipped default rule groups.
package config

import (
	"path/filepath"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// ResolvePath picks the config path: --config → TMSKIP_CONFIG →
// $XDG_CONFIG_HOME/tmskip/config.toml (only if absolute) →
// ~/.config/tmskip/config.toml. The first source that is set wins, even if
// the file is missing. Relative values are made absolute against cwd;
// symlinks are never resolved (the agent must get the link, not its target).
func ResolvePath(env sysenv.Env, flag, cwd string) string {
	abs := func(p string) string {
		if p == "~" || strings.HasPrefix(p, "~/") {
			p = env.Home + p[1:]
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		return filepath.Clean(p)
	}
	if flag != "" {
		return abs(flag)
	}
	if v := env.Getenv("TMSKIP_CONFIG"); v != "" {
		return abs(v)
	}
	if x := env.Getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "tmskip", "config.toml")
	}
	return filepath.Join(env.Home, ".config", "tmskip", "config.toml")
}

// ExpandHome expands a leading ~ or ~/ against home. Other values are
// returned unchanged.
func ExpandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return home + p[1:]
	}
	return p
}

package engine

import (
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

const testHome = "/Users/t"

// resolved parses, validates and compiles src against fs with $HOME=/Users/t.
func resolved(t *testing.T, fs fsys.FS, src string) *config.Resolved {
	t.Helper()
	f, err := config.Parse([]byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if err := f.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	env := sysenv.Env{Home: testHome, Getenv: func(string) string { return "" }}
	r, _, err := config.Compile(f, testHome+"/.config/tmskip/config.toml", env, fs)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return r
}

// shipped is a config with roots and every shipped group.
func shipped(roots ...string) string {
	return config.Template(roots)
}

// tree builds a memfs from paths: a trailing "/" makes a dir, otherwise a file.
func tree(paths ...string) *memfs.FS {
	m := memfs.New()
	m.Mkdirs(testHome)
	for _, p := range paths {
		if strings.HasSuffix(p, "/") {
			m.Mkdirs(strings.TrimSuffix(p, "/"))
		} else {
			m.WriteFile(p, []byte("x"))
		}
	}
	return m
}

func set(vals ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vals {
		m[v] = true
	}
	return m
}

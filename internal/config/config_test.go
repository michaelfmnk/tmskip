package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

func envWith(vars map[string]string) sysenv.Env {
	return sysenv.Env{Home: "/Users/t", Getenv: func(k string) string { return vars[k] }}
}

func TestResolvePathOrder(t *testing.T) {
	cases := []struct {
		name string
		flag string
		vars map[string]string
		want string
	}{
		{"default", "", nil, "/Users/t/.config/tmskip/config.toml"},
		{"xdg absolute", "", map[string]string{"XDG_CONFIG_HOME": "/x/cfg"}, "/x/cfg/tmskip/config.toml"},
		{"xdg relative ignored", "", map[string]string{"XDG_CONFIG_HOME": "cfg"}, "/Users/t/.config/tmskip/config.toml"},
		{"env beats xdg", "", map[string]string{"XDG_CONFIG_HOME": "/x/cfg", "TMSKIP_CONFIG": "/e/c.toml"}, "/e/c.toml"},
		{"flag beats env", "/f/c.toml", map[string]string{"TMSKIP_CONFIG": "/e/c.toml"}, "/f/c.toml"},
		{"relative flag made absolute", "dots/c.toml", nil, "/work/dots/c.toml"},
		{"relative env made absolute", "", map[string]string{"TMSKIP_CONFIG": "c.toml"}, "/work/c.toml"},
		{"no symlink resolution, just cleaning", "/f/../g/./c.toml", nil, "/g/c.toml"},
		{"tilde in flag", "~/dots/c.toml", nil, "/Users/t/dots/c.toml"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolvePath(envWith(c.vars), c.flag, "/work"); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func mustInvalid(t *testing.T, err error, substr string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %q", substr)
	}
	if !strings.Contains(err.Error(), substr) {
		t.Fatalf("error %q does not contain %q", err.Error(), substr)
	}
}

func TestParseUnknownKeys(t *testing.T) {
	cases := []struct {
		toml string
		want []string
	}{
		{"version = 1\n[[rule]]\nname = \"a\"\nnames = [\"x\"]\n[[rule]]\nname = \"b\"\nnames = [\"y\"]\n[[rule]]\nname = \"jvm-build\"\nnames = [\"build\"]\nparent-has = [\"pom.xml\"]\n",
			[]string{`unknown key "rule.parent-has"`, `rule #3 "jvm-build"`, "check spelling"}},
		{"version = 1\ncolour = true\n", []string{`unknown key "colour"`}},
		{"version = 1\n[schedule]\nevery = \"1h\"\n", []string{`unknown key "schedule.every"`}},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.toml))
		for _, w := range c.want {
			mustInvalid(t, err, w)
		}
	}
}

func TestParseTypeAndSyntaxErrors(t *testing.T) {
	_, err := Parse([]byte("version = 1\nroots = \"~/x\"\n"))
	mustInvalid(t, err, "roots")
	_, err = Parse([]byte("version = 1\nroots = [\n"))
	mustInvalid(t, err, "line")
}

func TestValidateTable(t *testing.T) {
	cases := []struct {
		name string
		toml string
		want string
	}{
		{"version absent", `roots = []`, `missing "version" — add version = 1`},
		{"version 2", `version = 2`, `unsupported version 2 — upgrade tmskip`},
		{"rule no name", "version = 1\n[[rule]]\nnames = [\"x\"]", `rule #1 has no name`},
		{"duplicate name", "version = 1\n[[rule]]\nname = \"a\"\nnames = [\"x\"]\n[[rule]]\nname = \"a\"\nnames = [\"y\"]", `duplicate rule name "a"`},
		{"paths with match", "version = 1\n[[rule]]\nname = \"a\"\npaths = [\"~/x\"]\nnames = [\"x\"]", `rule "a": paths can't be combined with names/contains/parent_has`},
		{"parent_has alone", "version = 1\n[[rule]]\nname = \"a\"\nparent_has = [\"pom.xml\"]", `rule "a": parent_has needs names or contains (it would exclude every sibling of the sentinel)`},
		{"nothing", "version = 1\n[[rule]]\nname = \"a\"", `rule "a": needs paths, or names/contains`},
		{"empty list", "version = 1\n[[rule]]\nname = \"a\"\nnames = []\ncontains = [\"x\"]", `rule "a": names is empty`},
		{"slash in name", "version = 1\n[[rule]]\nname = \"a\"\nnames = [\"a/b\"]", `rule "a": "a/b" is not a name (names are exact folder/file names)`},
		{"dotdot name", "version = 1\n[[rule]]\nname = \"a\"\ncontains = [\"..\"]", `rule "a": ".." is not a name`},
		{"empty name value", "version = 1\n[[rule]]\nname = \"a\"\nparent_has = [\"\"]\nnames = [\"x\"]", `rule "a": "" is not a name`},
		{"glob", "version = 1\n[[rule]]\nname = \"a\"\nnames = [\"*.egg-info\"]", `rule "a": globs aren't supported ("*.egg-info")`},
		{"relative root", "version = 1\nroots = [\"Developer\"]", `roots: "Developer" is relative — use an absolute path or ~/…`},
		{"relative keep", "version = 1\nkeep = [\"x/y\"]", `keep: "x/y" is relative`},
		{"relative fixed", "version = 1\n[[rule]]\nname = \"a\"\npaths = [\"models\"]", `"models" is relative`},
		{"tilde user", "version = 1\nroots = [\"~bob/x\"]", `"~bob/x": only ~ and ~/… are expanded`},
		{"skip slash", "version = 1\nskip = [\"a/b\"]", `skip: "a/b" is not a name`},
		{"skip glob", "version = 1\nskip = [\"*.git\"]", `skip: globs aren't supported`},
		{"interval short", "version = 1\n[schedule]\ninterval = \"5s\"", `schedule.interval "5s": must be a duration ≥ 5m like "1h"`},
		{"interval garbage", "version = 1\n[schedule]\ninterval = \"hourly\"", `schedule.interval "hourly": must be a duration`},
		{"interval fractional", "version = 1\n[schedule]\ninterval = \"10m500ms\"", `schedule.interval "10m500ms"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, err := Parse([]byte(c.toml))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = f.Validate()
			mustInvalid(t, err, c.want)
			if !strings.Contains(err.Error(), "config: ") {
				t.Fatalf("missing config: prefix: %q", err)
			}
		})
	}
}

func TestValidateCollectsAllErrors(t *testing.T) {
	f, err := Parse([]byte("roots = [\"rel\"]\n[[rule]]\nname = \"a\""))
	if err != nil {
		t.Fatal(err)
	}
	err = f.Validate()
	if n := strings.Count(err.Error(), "config: "); n != 3 {
		t.Fatalf("want 3 problems, got %d: %q", n, err)
	}
}

const specExample = `version = 1
roots   = ["~/Developer"]
skip    = [".git"]
keep    = ["~/Developer/notes"]
notify  = true

[schedule]
interval = "1h"

[[rule]]
name  = "node"
names = ["node_modules", ".next"]

[[rule]]
name     = "python-venv"
contains = ["pyvenv.cfg"]

[[rule]]
name       = "jvm-build"
names      = ["build", "target"]
parent_has = ["build.gradle", "build.gradle.kts", "settings.gradle",
              "settings.gradle.kts", "pom.xml"]

[[rule]]
name     = "nobackup-marker"
contains = [".nobackup"]

[[rule]]
name  = "my-models"
paths = ["~/.lmstudio/models"]
# enabled = false
`

func TestParseValidExample(t *testing.T) {
	f, err := Parse([]byte(specExample))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Validate(); err != nil {
		t.Fatal(err)
	}
	if *f.Version != 1 || !f.Notify || f.Schedule.Interval != "1h" || len(f.Rules) != 5 || f.Rules[2].ParentHas[4] != "pom.xml" {
		t.Fatalf("parsed = %+v", f)
	}
}

func TestLoadMissing(t *testing.T) {
	m := memfs.New()
	_, err := Load(m, "/Users/t/.config/tmskip/config.toml")
	var me *MissingError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v", err)
	}
	if !strings.Contains(err.Error(), `run "tmskip init"`) || !strings.Contains(err.Error(), "/Users/t/.config/tmskip/config.toml") {
		t.Fatalf("message = %q", err)
	}
}

func TestLoadDanglingSymlink(t *testing.T) {
	m := memfs.New()
	m.Symlink("/Users/t/dotfiles/tmskip.toml", "/Users/t/.config/tmskip/config.toml")
	_, err := Load(m, "/Users/t/.config/tmskip/config.toml")
	var me *MissingError
	if !errors.As(err, &me) {
		t.Fatalf("err = %v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "/Users/t/.config/tmskip/config.toml") || !strings.Contains(msg, "/Users/t/dotfiles/tmskip.toml") {
		t.Fatalf("message = %q", msg)
	}
}

func TestLoadInvalid(t *testing.T) {
	m := memfs.New()
	m.WriteFile("/c.toml", []byte("roots = []\n"))
	_, err := Load(m, "/c.toml")
	var ie *InvalidError
	if !errors.As(err, &ie) {
		t.Fatalf("err = %v", err)
	}
	m.WriteFile("/d.toml", []byte(specExample))
	if f, err := Load(m, "/d.toml"); err != nil || len(f.Rules) != 5 {
		t.Fatalf("valid load: %v", err)
	}
}

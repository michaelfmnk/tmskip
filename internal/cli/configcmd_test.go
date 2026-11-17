package cli

import (
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

func TestConfigDefaultsMarksPresence(t *testing.T) {
	e := newEnv(t)
	src := config.Template([]string{"~/Developer"})
	src = strings.Replace(src, `names = ["node_modules", ".next"]`, `names = ["node_modules"]`, 1)
	i, j := strings.Index(src, "# rust"), strings.Index(src, "# swiftpm")
	e.writeConfig(src[:i] + src[j:])
	e.mustRun(0, "config", "defaults")
	out := e.stdout.String()
	for _, want := range []string{"# node — changed in your config", "# python — in your config", "# rust — not in your config", `name       = "rust"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := lastLine(out); got != "→ add a group: tmskip config add <group>" {
		t.Fatalf("hint %q", got)
	}
	e.mustRun(0, "config", "defaults", "rust")
	if strings.Contains(e.stdout.String(), "# node") || !strings.Contains(e.stdout.String(), "# rust — not in your config") {
		t.Fatalf("single group:\n%s", e.stdout.String())
	}
}

func TestConfigDefaultsUnknownGroup(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mustRun(1, "config", "defaults", "cobol")
	if !strings.Contains(e.stderr.String(), "node, python, python-venv, jvm-build, rust, swiftpm, nobackup-marker, dev-caches") {
		t.Fatalf("stderr %q", e.stderr.String())
	}
}

func TestConfigAddAppendsVerbatim(t *testing.T) {
	e := newEnv(t)
	old := "# mine\n" + nodeConfig + "# trailing comment\n"
	e.writeConfig(old)
	e.mustRun(0, "config", "add", "rust", "swiftpm")
	got, _ := e.fs.ReadFile(cfgPath)
	if !strings.HasPrefix(string(got), old) || !strings.Contains(string(got), `name       = "swiftpm"`) {
		t.Fatalf("config:\n%s", got)
	}
	if lastLine(e.stdout.String()) != "→ preview: tmskip sync -n" {
		t.Fatalf("hint %q", e.stdout.String())
	}
}

func TestConfigAddRefusesExisting(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mustRun(1, "config", "add", "rust", "node")
	if !strings.Contains(e.stderr.String(), `rule "node" already exists — nothing added`) {
		t.Fatalf("stderr %q", e.stderr.String())
	}
	if got, _ := e.fs.ReadFile(cfgPath); string(got) != nodeConfig {
		t.Fatal("config changed")
	}
	e.mustRun(1, "config", "add", "cobol")
	e.mustRun(1, "config", "add")
}

func TestConfigWritesThroughSymlink(t *testing.T) {
	e := newEnv(t)
	e.fs.WriteFile(home+"/dotfiles/tmskip.toml", []byte(nodeConfig))
	e.fs.Symlink(home+"/dotfiles/tmskip.toml", cfgPath)
	e.mustRun(0, "config", "add", "rust")
	if info, _ := e.fs.Lstat(cfgPath); info.Type != fsys.TypeSymlink {
		t.Fatal("symlink replaced by a file")
	}
	if got, _ := e.fs.ReadFile(home + "/dotfiles/tmskip.toml"); !strings.Contains(string(got), `"rust"`) {
		t.Fatalf("target:\n%s", got)
	}
}

func TestConfigResetCreatesWhenMissing(t *testing.T) {
	e := newEnv(t)
	e.mkdirs(home+"/Developer/", home+"/code/")
	e.mustRun(0, "config", "reset")
	got, err := e.fs.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `roots = ["~/Developer", "~/code"]`) {
		t.Fatalf("config:\n%s", got)
	}
	if _, err := e.fs.Lstat(cfgPath + ".bak"); err == nil {
		t.Fatal("backup of nothing")
	}
}

func TestConfigResetBackupAndPrompt(t *testing.T) {
	setup := func(t *testing.T) *testEnv {
		e := newEnv(t)
		e.mkdirs(dev + "/")
		e.writeConfig(nodeConfig)
		return e
	}
	t.Run("yes", func(t *testing.T) {
		e := setup(t)
		e.tty, e.stdin = true, "y\n"
		e.mustRun(0, "config", "reset")
		if !strings.Contains(e.stdout.String(), "Replace "+cfgPath+"? A backup goes to "+cfgPath+".bak [y/N]") {
			t.Fatalf("prompt:\n%s", e.stdout.String())
		}
		if bak, _ := e.fs.ReadFile(cfgPath + ".bak"); string(bak) != nodeConfig {
			t.Fatalf("bak = %q", bak)
		}
		if got, _ := e.fs.ReadFile(cfgPath); !strings.Contains(string(got), `name  = "dev-caches"`) {
			t.Fatal("not reset")
		}
	})
	t.Run("no", func(t *testing.T) {
		e := setup(t)
		e.tty, e.stdin = true, "n\n"
		e.mustRun(0, "config", "reset")
		if got, _ := e.fs.ReadFile(cfgPath); string(got) != nodeConfig {
			t.Fatal("changed on no")
		}
	})
	t.Run("non-tty without --yes", func(t *testing.T) {
		e := setup(t)
		e.mustRun(1, "config", "reset")
		if !strings.Contains(e.stderr.String(), "--yes") {
			t.Fatalf("stderr %q", e.stderr.String())
		}
	})
	t.Run("--yes", func(t *testing.T) {
		e := setup(t)
		e.mustRun(0, "config", "reset", "--yes")
		if strings.Contains(e.stdout.String(), "[y/N]") {
			t.Fatal("prompted under --yes")
		}
		if bak, _ := e.fs.ReadFile(cfgPath + ".bak"); string(bak) != nodeConfig {
			t.Fatalf("bak = %q", bak)
		}
	})
}

func TestConfigPathShowsTarget(t *testing.T) {
	e := newEnv(t)
	e.mustRun(0, "config", "path")
	if !strings.HasPrefix(e.stdout.String(), cfgPath+" (does not exist)\n") {
		t.Fatalf("stdout %q", e.stdout.String())
	}
	e.fs.WriteFile(home+"/dotfiles/tmskip.toml", []byte(nodeConfig))
	e.fs.Symlink(home+"/dotfiles/tmskip.toml", cfgPath)
	e.mustRun(0, "config", "path")
	if !strings.HasPrefix(e.stdout.String(), cfgPath+" → "+home+"/dotfiles/tmskip.toml\n") {
		t.Fatalf("stdout %q", e.stdout.String())
	}
	if lastLine(e.stdout.String()) != "→ check it: tmskip doctor" {
		t.Fatal("hint")
	}
	e.vars["TMSKIP_CONFIG"] = "/etc/x.toml"
	e.mustRun(0, "config", "path")
	if !strings.HasPrefix(e.stdout.String(), "/etc/x.toml") {
		t.Fatalf("stdout %q", e.stdout.String())
	}
}

func TestConfigMutationsTakeLock(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.lock.Held = true
	e.mustRun(1, "config", "add", "rust")
	e.mustRun(1, "config", "reset", "--yes")
	if got, _ := e.fs.ReadFile(cfgPath); string(got) != nodeConfig {
		t.Fatal("changed while locked")
	}
	e.mustRun(0, "config", "defaults")
	e.mustRun(0, "config", "path")
}

func TestDetectRootsDedupesCase(t *testing.T) {
	e := newEnv(t, memfs.CaseInsensitive())
	e.mkdirs(home+"/Projects/", home+"/src/", home+"/notes/")
	e.fs.Symlink(home+"/src", home+"/code")
	got := detectRoots(e.Deps().Env, e.fs)
	want := []string{"~/Projects", "~/src"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v", got)
	}
}

// JetBrains/IDE default project folders are detected after the usual ones.
func TestDetectRootsIDEFolders(t *testing.T) {
	e := newEnv(t)
	e.mkdirs(home+"/IdeaProjects/", home+"/Developer/", home+"/eclipse-workspace/", home+"/GolandProjects/")
	got := detectRoots(e.Deps().Env, e.fs)
	want := []string{"~/Developer", "~/IdeaProjects", "~/GolandProjects", "~/eclipse-workspace"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v", got)
	}
}

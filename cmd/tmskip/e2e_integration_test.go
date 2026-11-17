//go:build integration

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// TestIntegrationEndToEnd builds tmskip and runs it against a fake $HOME in
// a temp dir: sync -n, sync, why, list, then a config edit and sync again.
// It never runs init, schedule or uninstall (those talk to the real
// launchd), and refuses to start unless everything is under the temp dir.
func TestIntegrationEndToEnd(t *testing.T) {
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sysTmp, _ := filepath.EvalSymlinks(os.TempDir())
	realHome, _ := os.UserHomeDir()
	if h, err := filepath.EvalSymlinks(realHome); err == nil {
		realHome = h
	}
	if !fsys.StrictlyUnder(tmp, sysTmp) || (realHome != "" && fsys.Under(tmp, realHome)) {
		t.Fatalf("refusing: %s is not under %s or is under $HOME", tmp, sysTmp)
	}

	bin := filepath.Join(tmp, "tmskip")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(tmp, "home")
	cfg := filepath.Join(home, ".config", "tmskip", "config.toml")
	dev := filepath.Join(home, "Developer")
	nm := filepath.Join(dev, "my app", "node_modules")
	for _, d := range []string{filepath.Dir(cfg), filepath.Join(nm, "left-pad"), filepath.Join(dev, "my app", "src")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(nm, "left-pad", "index.js"), make([]byte, 20000), 0o644)
	config := `version = 1
roots = ["~/Developer"]
skip = [".git"]
keep = []
notify = false

[[rule]]
name = "node"
names = ["node_modules"]
`
	if err := os.WriteFile(cfg, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(want int, args ...string) string {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"HOME=" + home, "TMSKIP_CONFIG=" + cfg, "PATH=/usr/bin:/bin", "NO_COLOR=1", "TMPDIR=" + tmp}
		out, err := cmd.Output()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != want {
			stderr := ""
			if ee != nil {
				stderr = string(ee.Stderr)
			}
			t.Fatalf("tmskip %v → exit %d, want %d\n%s\n%s", args, code, want, out, stderr)
		}
		return string(out)
	}
	has := func(p, name string) bool {
		_, err := fsys.OS{}.GetXattr(p, name)
		return err == nil
	}
	isExcluded := func(p string) string {
		out, err := exec.Command("/usr/bin/tmutil", "isexcluded", p).Output()
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	t.Cleanup(func() {
		fsys.OS{}.RemoveXattr(nm, exclusion.AttrName)
		fsys.OS{}.RemoveXattr(nm, exclusion.MarkerName)
	})

	out := run(0, "--version")
	if !strings.HasPrefix(out, "tmskip ") {
		t.Fatalf("--version: %q", out)
	}

	out = run(0, "sync", "-n")
	if !strings.Contains(out, "+ "+nm) || has(nm, exclusion.AttrName) || has(nm, exclusion.MarkerName) {
		t.Fatalf("dry run changed something or missed the folder:\n%s", out)
	}

	out = run(0, "sync")
	if !has(nm, exclusion.AttrName) || !has(nm, exclusion.MarkerName) {
		t.Fatalf("sync didn't mark and exclude:\n%s", out)
	}
	// Temp dirs are a Time Machine system exclusion, so isexcluded can't
	// tell our exclusion apart; it must at least agree.
	if !strings.Contains(isExcluded(nm), "[Excluded]") {
		t.Fatal("tmutil disagrees")
	}

	var why map[string]any
	if err := json.Unmarshal([]byte(run(0, "why", nm, "--json")), &why); err != nil {
		t.Fatal(err)
	}
	if why["owner"] != "tmskip" || why["rule"] != "node" || why["excluded_now"] != true {
		t.Fatalf("why = %v", why)
	}
	var list struct {
		Folders []struct{ Path, Rule, State string }
	}
	if err := json.Unmarshal([]byte(run(0, "list", "--json")), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Folders) != 1 || list.Folders[0].Path != nm || list.Folders[0].State != "excluded" {
		t.Fatalf("list = %+v", list)
	}

	os.WriteFile(cfg, []byte(strings.Replace(config, `names = ["node_modules"]`, "names = [\"node_modules\"]\nenabled = false", 1)), 0o644)
	out = run(0, "sync")
	if !strings.Contains(out, "- "+nm) || has(nm, exclusion.AttrName) || has(nm, exclusion.MarkerName) {
		t.Fatalf("re-include failed:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(home, "Library", "LaunchAgents")); err == nil {
		t.Fatal("sync touched the LaunchAgents folder")
	}
}

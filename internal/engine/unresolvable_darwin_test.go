//go:build darwin

package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// N1 on the real FS (temp dir only; marker xattr only; no Apply): a root
// spelled through a symlink inside a chmod-000 folder gives an empty plan.
func TestUnresolvableRootRealFS(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	d, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(d, "home")
	nm := filepath.Join(home, "Developer", "app", "node_modules")
	os.MkdirAll(nm, 0o755)
	os.MkdirAll(filepath.Join(home, "Locked"), 0o755)
	os.Symlink(filepath.Join(home, "Developer"), filepath.Join(home, "Locked", "dev"))
	fs := fsys.OS{}
	if err := fs.SetXattr(nm, exclusion.MarkerName, exclusion.MarkerValue); err != nil {
		t.Fatal(err)
	}
	os.Chmod(filepath.Join(home, "Locked"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(home, "Locked"), 0o755) })
	f, _ := config.Parse([]byte("version = 1\nroots = [\"~/Locked/dev\"]\n[[rule]]\nname = \"node\"\nnames = [\"node_modules\"]\n"))
	r, _, err := config.Compile(f, "/cfg", sysenv.Env{Home: home}, fs)
	if err != nil {
		t.Fatal(err)
	}
	ex := exclusion.New(fs)
	plan := Reconcile(r, fs, ex, Scan(r, fs, ex), []state.Owned{{Path: nm}})
	if len(plan.Actions) != 0 || len(plan.Problems) == 0 {
		t.Fatalf("ops=%q problems=%+v", ops(plan), plan.Problems)
	}
}

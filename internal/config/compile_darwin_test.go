//go:build darwin

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// Review R3 on the real filesystem: a keep path under a folder with mode
// 000 (read-only test in a temp dir; no xattrs).
func TestCompileUnreadableKeepRealFS(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	d, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(d, "home")
	os.MkdirAll(filepath.Join(home, "Developer"), 0o755)
	os.MkdirAll(filepath.Join(home, "Private", "x"), 0o755)
	os.Chmod(filepath.Join(home, "Private"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(home, "Private"), 0o755) })
	f, err := Parse([]byte("version = 1\nroots = [\"~/Developer\"]\nkeep = [\"~/Private/x\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	r, ws, err := Compile(f, "/cfg", sysenv.Env{Home: home}, fsys.OS{})
	var ie *InvalidError
	if errors.As(err, &ie) || err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(ws) != 1 || len(r.Unreadable) != 1 || r.Keep[0] != filepath.Join(home, "Private", "x") {
		t.Fatalf("warnings %v unreadable %v keep %v", ws, r.Unreadable, r.Keep)
	}
}

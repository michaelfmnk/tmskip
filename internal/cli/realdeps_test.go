package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoRealDepsOutsideAllowedFiles keeps real-world side effects behind the
// injected seams: only these files may read the environment, the clock or
// spawn processes.
func TestNoRealDepsOutsideAllowedFiles(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	allowed := func(rel string) bool {
		rel = filepath.ToSlash(rel)
		switch {
		case strings.HasPrefix(rel, "internal/sysenv/"),
			strings.HasPrefix(rel, "internal/runner/"),
			strings.HasPrefix(rel, "cmd/tmskip/"),
			rel == "internal/fsys/os_darwin.go",
			rel == "internal/state/lock_darwin.go":
			return true
		}
		return false
	}
	forbidden := regexp.MustCompile(`\bos\.Getenv\b|\bos\.LookupEnv\b|\bos\.Environ\b|\bos\.UserHomeDir\b|\bos\.Args\b|\bexec\.Command\b|\btime\.Now\b|\bos\.Getwd\b`)
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == ".superpowers" || name == "dist" || name == ".idea" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if allowed(rel) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if m := forbidden.FindString(line); m != "" {
				t.Errorf("%s:%d uses %s outside the allowed files", rel, i+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

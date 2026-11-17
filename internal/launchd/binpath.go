package launchd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

var cellar = regexp.MustCompile(`/Cellar/tmskip/[^/]+/`)

// BinaryPath is the stable path of the binary the user invoked: argv[0]
// made absolute (never symlink-resolved — /opt/homebrew/bin/tmskip, not its
// Cellar target), or looked up in PATH. warning is set when the path won't
// survive: a temp folder (go run) or a versioned Homebrew Cellar folder.
func BinaryPath(env sysenv.Env) (path, warning string, err error) {
	a := env.Args0
	switch {
	case a == "":
		return "", "", fmt.Errorf("can't tell where the tmskip binary is")
	case strings.Contains(a, "/"):
		if !filepath.IsAbs(a) {
			a = filepath.Join(env.Cwd, a)
		}
		path = filepath.Clean(a)
	default:
		if env.LookPath == nil {
			return "", "", fmt.Errorf("can't find %s in PATH", a)
		}
		p, err := env.LookPath(a)
		if err != nil {
			return "", "", fmt.Errorf("can't find %s in PATH: %w", a, err)
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(env.Cwd, p)
		}
		path = filepath.Clean(p)
	}
	tmpdirs := []string{"/private/var/folders/", "/var/folders/", "/tmp/", "/private/tmp/"}
	if env.Getenv != nil {
		if t := env.Getenv("TMPDIR"); t != "" {
			tmpdirs = append(tmpdirs, strings.TrimSuffix(t, "/")+"/")
		}
	}
	for _, t := range tmpdirs {
		if strings.HasPrefix(path, t) {
			return path, fmt.Sprintf("%s is in a temporary folder (go run?) — the agent would stop working when it's deleted; install tmskip and run \"tmskip schedule on\" from there", path), nil
		}
	}
	if cellar.MatchString(path) {
		return path, fmt.Sprintf("%s is a versioned Homebrew Cellar folder that \"brew cleanup\" deletes — run \"tmskip schedule on\" as $(brew --prefix)/bin/tmskip", path), nil
	}
	return path, "", nil
}

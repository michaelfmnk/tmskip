package launchd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/runner"
	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

var agent = Agent{
	Binary:   "/opt/homebrew/bin/tmskip",
	Config:   "/Users/t/My Dots & <Co>/tmskip.toml",
	Interval: time.Hour,
	LogPath:  "/Users/t/Library/Logs/tmskip/agent.log",
}

func TestPlistPath(t *testing.T) {
	if got := PlistPath("/Users/t"); got != "/Users/t/Library/LaunchAgents/dev.tmskip.agent.plist" {
		t.Fatal(got)
	}
}

func TestRenderParseRoundTrip(t *testing.T) {
	b, err := Render(agent)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"<key>Label</key>\n\t<string>dev.tmskip.agent</string>",
		"<string>sync</string>\n\t\t<string>-q</string>\n\t\t<string>--config</string>",
		"<string>/Users/t/My Dots &amp; &lt;Co&gt;/tmskip.toml</string>",
		"<key>StartInterval</key>\n\t<integer>3600</integer>",
		"<key>RunAtLoad</key>\n\t<false/>",
		"<key>ProcessType</key>\n\t<string>Background</string>",
		"<key>LowPriorityIO</key>\n\t<true/>",
		"<key>TMSKIP_AGENT</key>\n\t\t<string>1</string>",
		"<key>StandardOutPath</key>\n\t<string>/Users/t/Library/Logs/tmskip/agent.log</string>",
		"<key>StandardErrorPath</key>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("plist lacks %q:\n%s", want, s)
		}
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got != agent {
		t.Fatalf("round trip = %+v", got)
	}
	args, err := ProgramArguments(b)
	if err != nil || !reflect.DeepEqual(args, []string{agent.Binary, "sync", "-q", "--config", agent.Config}) {
		t.Fatalf("args = %v %v", args, err)
	}
}

// TestRenderIsValidPlist lints the rendered plist with plutil (read-only,
// a temp file; never loaded).
func TestRenderIsValidPlist(t *testing.T) {
	if _, err := os.Stat("/usr/bin/plutil"); err != nil {
		t.Skip("plutil not available")
	}
	b, _ := Render(agent)
	p := filepath.Join(t.TempDir(), "a.plist")
	os.WriteFile(p, b, 0o644)
	if out, err := exec.Command("/usr/bin/plutil", "-lint", p).CombinedOutput(); err != nil {
		t.Fatalf("plutil: %v\n%s", err, out)
	}
	out, err := exec.Command("/usr/bin/plutil", "-extract", "ProgramArguments.4", "raw", p).Output()
	if err != nil || strings.TrimSpace(string(out)) != agent.Config {
		t.Fatalf("plutil extract = %q %v", out, err)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not xml")); err == nil {
		t.Fatal("garbage parsed")
	}
	if _, err := Parse([]byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>Label</key><string>x</string></dict></plist>`)); err == nil {
		t.Fatal("plist without ProgramArguments parsed")
	}
}

func TestLaunchctlArgs(t *testing.T) {
	f := &runner.Fake{}
	l := New(f, 501)
	l.Bootstrap("/Users/t/Library/LaunchAgents/dev.tmskip.agent.plist")
	l.Bootout(Label)
	l.Loaded(Label)
	want := [][]string{
		{"/bin/launchctl", "bootstrap", "gui/501", "/Users/t/Library/LaunchAgents/dev.tmskip.agent.plist"},
		{"/bin/launchctl", "bootout", "gui/501/dev.tmskip.agent"},
		{"/bin/launchctl", "print", "gui/501/dev.tmskip.agent"},
	}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Fatalf("calls = %v", f.Calls)
	}
}

func TestBootstrapFailureIsError(t *testing.T) {
	f := &runner.Fake{Results: map[string]runner.Result{"/bin/launchctl bootstrap gui/501 /p": {Code: 5, Stderr: "Bootstrap failed: 5: Input/output error"}}}
	err := New(f, 501).Bootstrap("/p")
	if err == nil || !strings.Contains(err.Error(), "Input/output error") {
		t.Fatalf("err = %v", err)
	}
}

func TestBootoutNotLoadedIsNil(t *testing.T) {
	for _, code := range []int{3, 113} {
		f := &runner.Fake{Results: map[string]runner.Result{"/bin/launchctl bootout gui/501/dev.tmskip.agent": {Code: code}}}
		if err := New(f, 501).Bootout(Label); err != nil {
			t.Fatalf("code %d: %v", code, err)
		}
	}
	f := &runner.Fake{Results: map[string]runner.Result{"/bin/launchctl bootout gui/501/dev.tmskip.agent": {Code: 1, Stderr: "boom"}}}
	if err := New(f, 501).Bootout(Label); err == nil {
		t.Fatal("real failure swallowed")
	}
}

func TestLoadedFromPrintExitCode(t *testing.T) {
	key := "/bin/launchctl print gui/501/dev.tmskip.agent"
	cases := []struct {
		code   int
		loaded bool
		err    bool
	}{{0, true, false}, {113, false, false}, {1, false, true}}
	for _, c := range cases {
		f := &runner.Fake{Results: map[string]runner.Result{key: {Code: c.code}}}
		loaded, err := New(f, 501).Loaded(Label)
		if loaded != c.loaded || (err != nil) != c.err {
			t.Fatalf("code %d → %v %v", c.code, loaded, err)
		}
	}
}

func env(args0 string, vars map[string]string) sysenv.Env {
	return sysenv.Env{
		Home:   "/Users/t",
		Args0:  args0,
		Cwd:    "/Users/t/src/tmskip",
		Getenv: func(k string) string { return vars[k] },
		LookPath: func(name string) (string, error) {
			return "/opt/homebrew/bin/" + name, nil
		},
	}
}

func TestBinaryPath(t *testing.T) {
	cases := []struct{ args0, want string }{
		{"/opt/homebrew/bin/tmskip", "/opt/homebrew/bin/tmskip"}, // kept, not resolved to the Cellar
		{"tmskip", "/opt/homebrew/bin/tmskip"},                   // via PATH
		{"./tmskip", "/Users/t/src/tmskip/tmskip"},               // relative → absolute
	}
	for _, c := range cases {
		got, _, err := BinaryPath(env(c.args0, nil))
		if err != nil || got != c.want {
			t.Errorf("%q → %q %v", c.args0, got, err)
		}
	}
}

func TestBinaryPathWarnings(t *testing.T) {
	tmp := map[string]string{"TMPDIR": "/var/folders/xy/T/"}
	cases := []struct {
		args0 string
		warn  bool
	}{
		{"/var/folders/xy/T/go-build123/b001/exe/tmskip", true},
		{"/private/var/folders/xy/T/go-build/exe/tmskip", true},
		{"/tmp/tmskip", true},
		{"/opt/homebrew/Cellar/tmskip/1.2.0/bin/tmskip", true},
		{"/usr/local/Cellar/tmskip/1.2.0_1/bin/tmskip", true},
		{"/opt/homebrew/bin/tmskip", false},
		{"/Users/t/go/bin/tmskip", false},
	}
	for _, c := range cases {
		_, warn, _ := BinaryPath(env(c.args0, tmp))
		if (warn != "") != c.warn {
			t.Errorf("%s: warning %q", c.args0, warn)
		}
	}
}

func TestStale(t *testing.T) {
	m := memfs.New()
	m.WriteFile("/opt/homebrew/bin/tmskip", []byte("bin"))
	m.WriteFile("/usr/local/bin/tmskip", []byte("bin"))
	want := Agent{Binary: "/opt/homebrew/bin/tmskip", Config: "/Users/t/.config/tmskip/config.toml", Interval: time.Hour}
	if got := Stale(m, want, want); len(got) != 0 {
		t.Fatalf("fresh agent stale: %v", got)
	}
	cases := []struct {
		name   string
		mutate func(*Agent)
		want   string
	}{
		{"binary missing", func(a *Agent) { a.Binary = "/opt/homebrew/Cellar/tmskip/1.0/bin/tmskip" }, "missing"},
		{"binary differs", func(a *Agent) { a.Binary = "/usr/local/bin/tmskip" }, "/usr/local/bin/tmskip"},
		{"config differs", func(a *Agent) { a.Config = "/Users/t/old.toml" }, "/Users/t/old.toml"},
		{"interval differs", func(a *Agent) { a.Interval = 2 * time.Hour }, "every 2h"},
	}
	for _, c := range cases {
		have := want
		c.mutate(&have)
		got := Stale(m, have, want)
		if len(got) != 1 || !strings.Contains(got[0], c.want) {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestFake(t *testing.T) {
	f := &Fake{}
	var _ Launchd = f
	f.Bootstrap("/p")
	if ok, _ := f.Loaded(Label); !ok {
		t.Fatal("not loaded after bootstrap")
	}
	f.Bootout(Label)
	if ok, _ := f.Loaded(Label); ok {
		t.Fatal("loaded after bootout")
	}
	if !reflect.DeepEqual(f.Calls, []string{"bootstrap /p", "bootout " + Label}) {
		t.Fatalf("calls = %v", f.Calls)
	}
}

// launchd can't run a binary through a dangling symlink. Review R6.
func TestStaleDanglingBinarySymlink(t *testing.T) {
	m := memfs.New()
	m.Symlink("/Users/t/go/bin/tmskip", "/usr/local/bin/tmskip") // target deleted
	a := Agent{Binary: "/usr/local/bin/tmskip", Config: "/c.toml", Interval: time.Hour}
	got := Stale(m, a, a)
	if len(got) != 1 || !strings.Contains(got[0], "missing") {
		t.Fatalf("stale = %v", got)
	}
}

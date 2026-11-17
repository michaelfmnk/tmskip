package cli

import (
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func TestListStates(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/a/node_modules/", dev+"/b/node_modules/", dev+"/c/node_modules/", dev+"/d/vendor/", "/Volumes/USB/x/")
	e.mustRun(0, "sync")
	// b: the user removed the exclusion; c: deleted; d: marked but no rule matches.
	e.fs.RemoveXattr(dev+"/b/node_modules", "com.apple.metadata:com_apple_backup_excludeItem")
	e.fs.RemoveAll(dev + "/c/node_modules")
	e.fs.SetXattr(dev+"/d/vendor", "dev.tmskip.managed", []byte("1"))
	e.fs.Unmount("/Volumes/USB")
	st := e.loadState()
	st.Owned = append(st.Owned, state.Owned{Path: dev + "/d/vendor"}, state.Owned{Path: "/Volumes/USB/x/node_modules"})
	state.Save(e.fs, state.PathsFor(home), st)

	e.mustRun(0, "list")
	out := e.stdout.String()
	for _, want := range []string{
		dev + "/a/node_modules  (node)",
		dev + "/b/node_modules  (node) [marked_only]",
		dev + "/c/node_modules  [missing]",
		dev + "/d/vendor  [no_longer_matches]",
		"/Volumes/USB/x/node_modules  [unmounted]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := lastLine(out); got != "→ why one is excluded: tmskip why <path>" {
		t.Fatalf("hint %q", got)
	}
	e.mustRun(0, "list", "--json")
	golden(t, "list.golden.json", e.stdout.Bytes())
}

func TestListAliasLs(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/a/node_modules/")
	e.mustRun(0, "sync")
	e.mustRun(0, "list")
	list := e.stdout.String()
	e.mustRun(0, "ls")
	if e.stdout.String() != list {
		t.Fatalf("ls differs:\n%s\nvs\n%s", e.stdout.String(), list)
	}
}

func TestListEmpty(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/")
	e.mustRun(0, "list")
	if got := lastLine(e.stdout.String()); got != "→ nothing managed yet — preview: tmskip sync -n" {
		t.Fatalf("hint %q", got)
	}
}

func TestListSizesMeasuresLive(t *testing.T) {
	e := newEnv(t)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/a/node_modules/")
	e.mustRun(0, "sync")
	e.fs.WriteFile(dev+"/a/node_modules/big", make([]byte, 3_000_000))
	e.mustRun(0, "list", "--sizes")
	if !strings.Contains(e.stdout.String(), dev+"/a/node_modules  (node, 3.0 MB)") {
		t.Fatalf("stdout:\n%s", e.stdout.String())
	}
}

// Review R2: a hint stored in an old letter case is listed once, canonical.
func TestListCanonicalisesHints(t *testing.T) {
	e := newEnv(t, memfs.CaseInsensitive())
	e.writeConfig(nodeConfig)
	e.mkdirs(dev + "/a/node_modules/")
	e.mustRun(0, "sync")
	st := e.loadState()
	st.Owned = append(st.Owned, state.Owned{Path: home + "/developer/a/node_modules"})
	state.Save(e.fs, state.PathsFor(home), st)
	e.mustRun(0, "list")
	out := e.stdout.String()
	if strings.Count(out, "node_modules") != 1 || !strings.Contains(out, dev+"/a/node_modules  (node)") {
		t.Fatalf("list:\n%s", out)
	}
}

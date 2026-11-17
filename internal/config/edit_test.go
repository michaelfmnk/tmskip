package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

const userConfig = `# my tmskip config
version = 1
roots = ["~/Developer"] # where code lives

[[rule]]
name = "node"   # js
names = ["node_modules"]`

func TestAppendGroupsVerbatim(t *testing.T) {
	rust, _ := FindGroup("rust")
	swift, _ := FindGroup("swiftpm")
	out, err := AppendGroups([]byte(userConfig), []Group{rust, swift})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, []byte(userConfig)) {
		t.Fatalf("old text not an exact prefix:\n%s", out)
	}
	if !strings.HasSuffix(string(out), "\n\n"+rust.Text+"\n"+swift.Text) {
		t.Fatalf("appended text:\n%s", out)
	}
	f, err := Parse(out)
	if err != nil || f.Validate() != nil || len(f.Rules) != 3 {
		t.Fatalf("result invalid: %v", err)
	}
}

func TestAppendGroupsRefusesExisting(t *testing.T) {
	node, _ := FindGroup("node")
	rust, _ := FindGroup("rust")
	_, err := AppendGroups([]byte(userConfig), []Group{rust, node})
	if err == nil || err.Error() != `rule "node" already exists — nothing added` {
		t.Fatalf("err = %v", err)
	}
	_, err = AppendGroups([]byte("version = \n"), []Group{rust})
	if err == nil {
		t.Fatal("appended to an unparsable config")
	}
}

func TestWriteThroughSymlink(t *testing.T) {
	m := memfs.New()
	m.WriteFile("/Users/t/dotfiles/tmskip.toml", []byte("old"))
	m.WriteFileAtomic("/Users/t/dotfiles/tmskip.toml", []byte("old"), 0o600)
	m.Symlink("../../dotfiles/tmskip.toml", "/Users/t/.config/tmskip/config.toml")
	if err := WriteThrough(m, "/Users/t/.config/tmskip/config.toml", []byte("new")); err != nil {
		t.Fatal(err)
	}
	if info, _ := m.Lstat("/Users/t/.config/tmskip/config.toml"); info.Type != fsys.TypeSymlink {
		t.Fatalf("link replaced: %+v", info)
	}
	got, _ := m.ReadFile("/Users/t/dotfiles/tmskip.toml")
	info, _ := m.Lstat("/Users/t/dotfiles/tmskip.toml")
	if string(got) != "new" || info.Mode.Perm() != 0o600 {
		t.Fatalf("target = %q mode %v", got, info.Mode)
	}
	if err := WriteThrough(m, "/Users/t/new/dir/c.toml", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.ReadFile("/Users/t/new/dir/c.toml"); string(got) != "x" {
		t.Fatal("new file not created")
	}
}

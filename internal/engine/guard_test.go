package engine

import (
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

func TestBlocked(t *testing.T) {
	m := tree("/Users/t/Developer/x/build/keepme/", "/Users/t/.npm/", "/Users/t/Developer/app/")
	r := resolved(t, m, `version = 1
roots = ["~/Developer"]
keep = ["~/Developer/x/build/keepme"]
[[rule]]
name = "caches"
paths = ["~/.npm", "~", "/Users"]
`)
	cases := []struct {
		path, reason string
		blocked      bool
	}{
		{"/Users/t", "is your home folder", true},
		{"/Users/t/Developer", "is a scan root", true},
		{"/Users", "contains scan root /Users/t/Developer", true},
		{"/", "contains scan root /Users/t/Developer", true},
		{"/Users/t/Developer/x/build/keepme", "is kept by /Users/t/Developer/x/build/keepme", true},
		{"/Users/t/Developer/x/build/keepme/sub", "is kept by /Users/t/Developer/x/build/keepme", true},
		{"/Users/t/Developer/x/build", "contains kept path /Users/t/Developer/x/build/keepme", true},
		{"/Users/t/Developer/x", "contains kept path /Users/t/Developer/x/build/keepme", true},
		{"/Users/t/Documents/node_modules", "is outside roots and fixed paths", true},
		{"/Users/t/Developerx/node_modules", "is outside roots and fixed paths", true},
		{"/Users/t/Developer/app/node_modules", "", false},
		{"/Users/t/.npm", "", false},
	}
	for _, c := range cases {
		reason, blocked := Blocked(r, c.path)
		if blocked != c.blocked || reason != c.reason {
			t.Errorf("Blocked(%q) = %q %v, want %q %v", c.path, reason, blocked, c.reason, c.blocked)
		}
	}
}

func TestBlockedHomeThroughSymlink(t *testing.T) {
	m := memfs.New()
	m.Mkdirs("/Volumes/Data/t/Developer")
	m.Symlink("/Volumes/Data/t", "/Users/t")
	r := resolved(t, m, `version = 1
roots = ["~/Developer"]
[[rule]]
name = "oops"
paths = ["~", "/Users/t"]
`)
	if got := r.Rules[0].Fixed; len(got) != 2 || got[0] != "/Volumes/Data/t" || got[1] != "/Volumes/Data/t" {
		t.Fatalf("fixed = %v", got)
	}
	for _, p := range r.Rules[0].Fixed {
		if reason, blocked := Blocked(r, p); !blocked || reason != "is your home folder" {
			t.Fatalf("Blocked(%q) = %q %v", p, reason, blocked)
		}
	}
}

// Excluding an ancestor of $HOME would exclude $HOME (exclusions are
// recursive). Review R5.
func TestBlockedAncestorOfHome(t *testing.T) {
	m := tree("/Volumes/Data/t/x/", "/Users/t/")
	r := resolved(t, m, `version = 1
[[rule]]
name = "oops"
paths = ["/Users", "/"]
`)
	for _, p := range []string{"/Users", "/"} {
		if reason, blocked := Blocked(r, p); !blocked || reason != "contains your home folder" {
			t.Errorf("Blocked(%q) = %q %v", p, reason, blocked)
		}
	}
	if res := Scan(r, m, exclusion.New(m)); len(res.Desired) != 0 {
		t.Fatalf("desired = %v", desired(res))
	}
}

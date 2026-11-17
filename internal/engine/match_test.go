package engine

import "testing"

func TestMatchDir(t *testing.T) {
	m := tree("/Users/t/Developer/")
	base := `version = 1
roots = ["~/Developer"]
`
	cases := []struct {
		name     string
		rules    string
		dir      string
		children map[string]bool
		siblings map[string]bool
		want     Match
		ok       bool
	}{
		{"names hit", `[[rule]]
name = "node"
names = ["node_modules", ".next"]`, "node_modules", nil, set("node_modules", "package.json"), Match{"node", "names", "node_modules"}, true},
		{"names case-sensitive", `[[rule]]
name = "node"
names = ["node_modules"]`, "Node_Modules", nil, nil, Match{}, false},
		{"contains any child", `[[rule]]
name = "python-venv"
contains = ["pyvenv.cfg"]`, "kokoro-env", set("bin", "pyvenv.cfg"), nil, Match{"python-venv", "contains", "pyvenv.cfg"}, true},
		{"contains miss", `[[rule]]
name = "python-venv"
contains = ["pyvenv.cfg"]`, "kokoro-env", set("bin"), nil, Match{}, false},
		{"parent_has gate", `[[rule]]
name = "jvm-build"
names = ["build", "target"]
parent_has = ["build.gradle", "pom.xml"]`, "build", nil, set("build", "build.gradle", "src"), Match{"jvm-build", "parent_has", "build.gradle"}, true},
		{"parent_has missing", `[[rule]]
name = "jvm-build"
names = ["build"]
parent_has = ["build.gradle"]`, "build", nil, set("build", "docs"), Match{}, false},
		{"contains AND parent_has: both", `[[rule]]
name = "x"
contains = ["X"]
parent_has = ["Y"]`, "d", set("X"), set("Y", "d"), Match{"x", "parent_has", "Y"}, true},
		{"contains AND parent_has: only contains", `[[rule]]
name = "x"
contains = ["X"]
parent_has = ["Y"]`, "d", set("X"), set("d"), Match{}, false},
		{"contains AND parent_has: only parent", `[[rule]]
name = "x"
contains = ["X"]
parent_has = ["Y"]`, "d", set("Z"), set("Y", "d"), Match{}, false},
		{"names AND contains", `[[rule]]
name = "x"
names = ["a"]
contains = ["X"]`, "b", set("X"), nil, Match{}, false},
		{"first rule wins", `[[rule]]
name = "jvm-build"
names = ["target"]
parent_has = ["pom.xml"]
[[rule]]
name = "rust"
names = ["target"]
parent_has = ["Cargo.toml", "pom.xml"]`, "target", nil, set("pom.xml", "Cargo.toml", "target"), Match{"jvm-build", "parent_has", "pom.xml"}, true},
		{"disabled rule never matches", `[[rule]]
name = "node"
names = ["node_modules"]
enabled = false`, "node_modules", nil, nil, Match{}, false},
		{"fixed rules don't match by name", `[[rule]]
name = "c"
paths = ["~/Developer"]`, "Developer", nil, nil, Match{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := resolved(t, m, base+c.rules)
			got, ok := MatchDir(r, c.dir, c.children, c.siblings)
			if ok != c.ok || got != c.want {
				t.Fatalf("got %+v %v, want %+v %v", got, ok, c.want, c.ok)
			}
		})
	}
}

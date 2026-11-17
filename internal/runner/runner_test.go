package runner

import (
	"reflect"
	"testing"
)

var _ Runner = Exec{}
var _ Runner = (*Fake)(nil)

func TestExecCapturesOutputAndExitCode(t *testing.T) {
	out, _, code, err := Exec{}.Run("/bin/sh", "-c", "echo hi; exit 3")
	if err != nil || string(out) != "hi\n" || code != 3 {
		t.Fatalf("out=%q code=%d err=%v", out, code, err)
	}
	if _, _, _, err := (Exec{}).Run("/nonexistent/binary"); err == nil {
		t.Fatal("missing binary should be an error")
	}
}

func TestFakeRecordsAndScripts(t *testing.T) {
	f := &Fake{Results: map[string]Result{
		"/bin/launchctl print gui/501/x": {Code: 113},
		"/usr/bin/tmutil":                {Stdout: "dest"},
	}}
	_, _, code, _ := f.Run("/bin/launchctl", "print", "gui/501/x")
	out, _, _, _ := f.Run("/usr/bin/tmutil", "destinationinfo")
	_, _, other, _ := f.Run("/usr/bin/open", "x")
	if code != 113 || string(out) != "dest" || other != 0 {
		t.Fatalf("code=%d out=%q other=%d", code, out, other)
	}
	want := [][]string{{"/bin/launchctl", "print", "gui/501/x"}, {"/usr/bin/tmutil", "destinationinfo"}, {"/usr/bin/open", "x"}}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Fatalf("calls = %v", f.Calls)
	}
}

package notify

import (
	"reflect"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/runner"
)

func TestOsascriptArgs(t *testing.T) {
	f := &runner.Fake{}
	n := Osascript{R: f}
	title, body := `tmskip "quoted"`, `can't read; Run "tmskip doctor" & \ more`
	if err := n.Notify(title, body); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"/usr/bin/osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		title, body}}
	if !reflect.DeepEqual(f.Calls, want) {
		t.Fatalf("calls = %q", f.Calls)
	}
}

func TestOsascriptFailure(t *testing.T) {
	f := &runner.Fake{Results: map[string]runner.Result{"/usr/bin/osascript": {Code: 1, Stderr: "nope"}}}
	if err := (Osascript{R: f}).Notify("a", "b"); err == nil {
		t.Fatal("failure swallowed")
	}
}

func TestFake(t *testing.T) {
	f := &Fake{}
	var _ Notifier = f
	f.Notify("t", "b")
	if !reflect.DeepEqual(f.Sent, []Message{{"t", "b"}}) {
		t.Fatalf("sent = %v", f.Sent)
	}
}

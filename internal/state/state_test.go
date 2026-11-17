package state

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys/memfs"
)

var now = time.Date(2026, 11, 16, 10, 0, 0, 0, time.FixedZone("CET", 3600))

func TestPathsFor(t *testing.T) {
	p := PathsFor("/Users/t")
	want := Paths{
		Dir:      "/Users/t/Library/Application Support/tmskip",
		File:     "/Users/t/Library/Application Support/tmskip/state.json",
		Lock:     "/Users/t/Library/Application Support/tmskip/lock",
		LogDir:   "/Users/t/Library/Logs/tmskip",
		Log:      "/Users/t/Library/Logs/tmskip/tmskip.log",
		AgentLog: "/Users/t/Library/Logs/tmskip/agent.log",
	}
	if p != want {
		t.Fatalf("paths = %+v", p)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	s, warn, err := Load(memfs.New(), PathsFor("/Users/t"))
	if err != nil || warn != "" || s == nil || s.Version != 1 || len(s.Owned) != 0 || s.LastRun != nil {
		t.Fatalf("s=%+v warn=%q err=%v", s, warn, err)
	}
}

func TestLoadCorruptWarnsAndStartsFresh(t *testing.T) {
	m := memfs.New()
	p := PathsFor("/Users/t")
	m.WriteFile(p.File, []byte("{not json"))
	s, warn, err := Load(m, p)
	if err != nil || s == nil || len(s.Owned) != 0 {
		t.Fatalf("s=%+v err=%v", s, err)
	}
	if warn != "state unreadable, starting fresh (markers are still the proof)" {
		t.Fatalf("warn = %q", warn)
	}
}

func TestLoadNewerVersionErrors(t *testing.T) {
	m := memfs.New()
	p := PathsFor("/Users/t")
	m.WriteFile(p.File, []byte(`{"version": 2}`))
	_, _, err := Load(m, p)
	if err == nil || !strings.Contains(err.Error(), "newer tmskip") {
		t.Fatalf("err = %v", err)
	}
}

func TestSaveRoundTripAtomic(t *testing.T) {
	m := memfs.New()
	p := PathsFor("/Users/t")
	n := int64(412000000)
	in := &State{
		Owned: []Owned{{Path: "/Users/t/Developer/Проєкти/app/node_modules", Bytes: &n}, {Path: "/Users/t/x y/build"}},
		LastRun: &Run{At: now, Scheduled: true, ExitCode: 3, Excluded: 3, Included: 1, Unchanged: 40,
			Problems: []RunProblem{{Path: "/Users/t/Documents/x", Error: "operation not permitted", Denied: true}}},
		LastScheduled: &Scheduled{At: now, FDA: "granted"},
		Notified:      []string{"fda-denied"},
	}
	if err := Save(m, p, in); err != nil {
		t.Fatal(err)
	}
	raw, _ := m.ReadFile(p.File)
	for _, want := range []string{`"version": 1`, `"bytes": 412000000`, `"last_scheduled"`, `"fda": "granted"`, `"exit_code": 3`, "Проєкти"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("state.json lacks %s:\n%s", want, raw)
		}
	}
	if strings.Contains(string(raw), `"path": "/Users/t/x y/build", "bytes"`) {
		t.Error("unknown bytes should be omitted")
	}
	out, warn, err := Load(m, p)
	if err != nil || warn != "" {
		t.Fatal(err, warn)
	}
	in.Version = 1
	if !reflect.DeepEqual(out.Owned, in.Owned) || !out.LastRun.At.Equal(now) || out.LastScheduled.FDA != "granted" || !reflect.DeepEqual(out.Notified, in.Notified) {
		t.Fatalf("round trip: %+v", out)
	}
}

func TestLogFormatAndRotation(t *testing.T) {
	m := memfs.New()
	p := PathsFor("/Users/t")
	if err := AppendLog(m, p, now, "sync manual exit=0 +1 -0 =0 problems=0", "+ /Users/t/a b/node_modules (node)"); err != nil {
		t.Fatal(err)
	}
	got, _ := m.ReadFile(p.Log)
	want := "2026-11-16T10:00:00+01:00 sync manual exit=0 +1 -0 =0 problems=0\n2026-11-16T10:00:00+01:00 + /Users/t/a b/node_modules (node)\n"
	if string(got) != want {
		t.Fatalf("log = %q", got)
	}
	m.WriteFile(p.Log, make([]byte, 1<<20+1))
	if err := AppendLog(m, p, now, "after rotation"); err != nil {
		t.Fatal(err)
	}
	if info, err := m.Lstat(p.Log + ".1"); err != nil || info.Size != 1<<20+1 {
		t.Fatalf("rotated: %+v %v", info, err)
	}
	if got, _ := m.ReadFile(p.Log); string(got) != "2026-11-16T10:00:00+01:00 after rotation\n" {
		t.Fatalf("new log = %q", got)
	}
}

func TestFlockSecondHolderFails(t *testing.T) {
	lock := filepath.Join(t.TempDir(), "sub", "lock")
	var l FlockLocker
	unlock, ok, err := l.TryLock(lock)
	if err != nil || !ok {
		t.Fatalf("first: %v %v", ok, err)
	}
	_, ok2, err := l.TryLock(lock)
	if err != nil || ok2 {
		t.Fatalf("second: %v %v", ok2, err)
	}
	unlock()
	unlock3, ok3, err := l.TryLock(lock)
	if err != nil || !ok3 {
		t.Fatalf("after unlock: %v %v", ok3, err)
	}
	unlock3()
}

func TestFakeLocker(t *testing.T) {
	f := &FakeLocker{Held: true}
	if _, ok, _ := f.TryLock("/x"); ok {
		t.Fatal("held fake granted the lock")
	}
	f.Held = false
	unlock, ok, _ := f.TryLock("/x")
	if !ok || !f.Locked || f.Taken != 1 {
		t.Fatalf("fake = %+v", f)
	}
	unlock()
	if f.Locked {
		t.Fatal("unlock didn't release")
	}
}

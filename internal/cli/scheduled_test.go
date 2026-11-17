package cli

import (
	"strings"
	"syscall"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/notify"
)

const tccDB = home + "/Library/Application Support/com.apple.TCC/TCC.db"

func withNotifier(e *testEnv) *notify.Fake {
	n := &notify.Fake{}
	prev := e.deps
	e.deps = func(d *Deps) {
		if prev != nil {
			prev(d)
		}
		d.Notifier = n
	}
	return n
}

func scheduled(e *testEnv) { e.vars["TMSKIP_AGENT"] = "1" }

func scheduledSync(e *testEnv, want int) {
	e.t.Helper()
	e.mustRun(want, "sync", "-q", "--config", cfgPath)
}

func TestScheduledRecordsFDA(t *testing.T) {
	cases := []struct {
		name  string
		setup func(e *testEnv)
		want  string
	}{
		{"granted", func(e *testEnv) { e.fs.WriteFile(tccDB, []byte("db")) }, "granted"},
		{"denied", func(e *testEnv) { e.fs.WriteFile(tccDB, []byte("db")); e.fs.Deny(tccDB, syscall.EPERM) }, "denied"},
		{"unknown", func(e *testEnv) {}, "unknown"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			withNotifier(e)
			e.writeConfig(nodeConfig)
			e.mkdirs(dev + "/")
			c.setup(e)
			scheduled(e)
			scheduledSync(e, 0)
			st := e.loadState()
			if st.LastScheduled == nil || st.LastScheduled.FDA != c.want || !st.LastScheduled.At.Equal(fixedTime) {
				t.Fatalf("last scheduled = %+v", st.LastScheduled)
			}
			if !st.LastRun.Scheduled {
				t.Fatal("run not recorded as scheduled")
			}
		})
	}
}

func TestManualRunDoesNotProbeOrNotify(t *testing.T) {
	e := newEnv(t)
	n := withNotifier(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/", dev+"/locked/")
	e.fs.Deny(dev+"/locked", syscall.EPERM)
	e.fs.WriteFile(tccDB, []byte("db"))
	e.mustRun(3, "sync")
	if st := e.loadState(); st.LastScheduled != nil || len(st.Notified) != 0 {
		t.Fatalf("state = %+v", st)
	}
	if len(n.Sent) != 0 {
		t.Fatalf("sent = %v", n.Sent)
	}
}

func TestNotifyOncePerNewProblem(t *testing.T) {
	e := newEnv(t)
	n := withNotifier(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/", dev+"/locked/")
	e.fs.Deny(dev+"/locked", syscall.EACCES)
	scheduled(e)
	scheduledSync(e, 3)
	scheduledSync(e, 3)
	if len(n.Sent) != 1 {
		t.Fatalf("sent %d: %v", len(n.Sent), n.Sent)
	}
	if n.Sent[0].Body != `tmskip couldn't read 1 folder. Run "tmskip status".` {
		t.Fatalf("body %q", n.Sent[0].Body)
	}
	// Fixed, then back: notifies again.
	e.fs.RemoveAll(dev + "/locked")
	scheduledSync(e, 0)
	e.mkdirs(dev + "/locked/")
	e.fs.Deny(dev+"/locked", syscall.EACCES)
	scheduledSync(e, 3)
	if len(n.Sent) != 2 {
		t.Fatalf("sent %d: %v", len(n.Sent), n.Sent)
	}
}

func TestFDADeniedMessage(t *testing.T) {
	e := newEnv(t)
	n := withNotifier(e)
	e.writeConfig(nodeConfig)
	e.mkdirs(dev+"/", dev+"/a/", dev+"/b/")
	e.fs.Deny(dev+"/a", syscall.EPERM)
	e.fs.Deny(dev+"/b", syscall.EPERM)
	scheduled(e)
	scheduledSync(e, 3)
	if len(n.Sent) != 1 || n.Sent[0].Title != "tmskip" ||
		n.Sent[0].Body != `tmskip can't read some folders — Full Disk Access is off for the scheduled run. Run "tmskip doctor".` {
		t.Fatalf("sent = %v", n.Sent)
	}
	if st := e.loadState(); !strings.Contains(strings.Join(st.Notified, ","), "fda-denied") {
		t.Fatalf("notified = %v", st.Notified)
	}
}

func TestNotifyFalseSuppresses(t *testing.T) {
	e := newEnv(t)
	n := withNotifier(e)
	e.writeConfig(strings.Replace(nodeConfig, "notify = true", "notify = false", 1))
	e.mkdirs(dev+"/", dev+"/locked/")
	e.fs.Deny(dev+"/locked", syscall.EPERM)
	scheduled(e)
	scheduledSync(e, 3)
	if len(n.Sent) != 0 {
		t.Fatalf("sent = %v", n.Sent)
	}
}

func TestInvalidConfigNotifiesEvenIfNotifyFalse(t *testing.T) {
	e := newEnv(t)
	n := withNotifier(e)
	e.writeConfig("version = 1\nnotify = false\nroots = [\"rel\"]\n")
	scheduled(e)
	scheduledSync(e, 2)
	scheduledSync(e, 2)
	if len(n.Sent) != 1 || !strings.Contains(n.Sent[0].Body, "config") {
		t.Fatalf("sent = %v", n.Sent)
	}
	st := e.loadState()
	if st.LastRun == nil || st.LastRun.ExitCode != 2 || !strings.Contains(st.LastRun.Error, "relative") {
		t.Fatalf("last run = %+v", st.LastRun)
	}
}

// Review R4: a scheduled run that can't read its config (a dotfiles symlink
// into ~/Documents without Full Disk Access) does nothing; it must say so
// once, whatever notify says (there is no readable setting to honour).
func TestScheduledUnreadableConfigNotifies(t *testing.T) {
	cases := []struct {
		errno syscall.Errno
		body  string
		key   string
	}{
		{syscall.EPERM, "Full Disk Access is off for the scheduled run", "fda-denied"},
		{syscall.EACCES, "can't read its config", "config-unreadable"},
	}
	for _, c := range cases {
		t.Run(c.errno.Error(), func(t *testing.T) {
			e := newEnv(t)
			n := withNotifier(e)
			e.writeConfig(strings.Replace(nodeConfig, "notify = true", "notify = false", 1))
			e.fs.Deny(cfgPath, c.errno)
			scheduled(e)
			scheduledSync(e, 1)
			scheduledSync(e, 1)
			if len(n.Sent) != 1 || !strings.Contains(n.Sent[0].Body, c.body) {
				t.Fatalf("sent = %+v", n.Sent)
			}
			if st := e.loadState(); !strings.Contains(strings.Join(st.Notified, ","), c.key) {
				t.Fatalf("notified = %v", st.Notified)
			}
		})
	}
}

// Review R4: a failure after the config loaded honours notify.
func TestScheduledRuntimeErrorHonoursNotify(t *testing.T) {
	for _, on := range []bool{true, false} {
		e := newEnv(t)
		n := withNotifier(e)
		cfg := nodeConfig
		if !on {
			cfg = strings.Replace(cfg, "notify = true", "notify = false", 1)
		}
		e.writeConfig(cfg)
		e.mkdirs(dev + "/a/node_modules/")
		e.fs.FailWrite(home+"/Library/Application Support/tmskip/state.json", syscall.ENOSPC)
		scheduled(e)
		scheduledSync(e, 1)
		want := 0
		if on {
			want = 1
		}
		if len(n.Sent) != want || (on && !strings.Contains(n.Sent[0].Body, "scheduled run failed")) {
			t.Fatalf("notify=%v: sent = %+v", on, n.Sent)
		}
	}
}

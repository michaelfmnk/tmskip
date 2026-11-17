package engine

import (
	"reflect"
	"sync"
	"testing"

	"github.com/michaelfmnk/tmskip/internal/exclusion"
)

type scanEvent struct {
	Dir            string
	Folders, Found int
}

type recorder struct {
	mu       sync.Mutex
	scans    []scanEvent
	measured [][2]int
}

func (r *recorder) Scanning(dir string, folders, found int) {
	r.scans = append(r.scans, scanEvent{dir, folders, found})
}

func (r *recorder) Measured(done, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.measured = append(r.measured, [2]int{done, total})
}

func TestScanEmitsProgress(t *testing.T) {
	m := tree(D+"/a/node_modules/x/", D+"/b/src/", D+"/.git/objects/")
	r := resolved(t, m, `version = 1
roots = ["~/Developer"]
skip = [".git"]
[[rule]]
name = "node"
names = ["node_modules"]
`)
	rec := &recorder{}
	res := ScanWith(r, m, exclusion.New(m), rec)
	want := []scanEvent{
		{D, 1, 0},        // the root
		{D + "/a", 2, 0}, // visited folders, in walk order
		{D + "/a/node_modules", 3, 0},
		{D + "/b", 4, 1}, // found counts what's desired so far
		{D + "/b/src", 5, 1},
	}
	if !reflect.DeepEqual(rec.scans, want) {
		t.Fatalf("events = %+v", rec.scans)
	}
	if len(res.Desired) != 1 {
		t.Fatalf("desired = %v", desired(res))
	}
	// nil progress is allowed and Scan is ScanWith(…, nil).
	if got := Scan(r, m, exclusion.New(m)); len(got.Desired) != 1 {
		t.Fatal("Scan without progress")
	}
}

func TestSizesEmitsProgress(t *testing.T) {
	m := tree()
	m.WriteFile("/a/f", make([]byte, 4096))
	m.WriteFile("/b/f", make([]byte, 4096))
	m.WriteFile("/c/f", make([]byte, 4096))
	rec := &recorder{}
	got := SizesWith(m, []string{"/a", "/b", "/c", "/missing"}, 3, rec)
	if len(got) != 3 {
		t.Fatalf("sizes = %v", got)
	}
	want := [][2]int{{1, 4}, {2, 4}, {3, 4}, {4, 4}}
	if !reflect.DeepEqual(rec.measured, want) {
		t.Fatalf("measured = %v", rec.measured)
	}
}

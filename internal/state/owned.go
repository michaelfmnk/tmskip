// Package state is tmskip's state file, run log and lock.
package state

// Owned is one entry of the owned-path hint list: where tmskip last saw a
// folder it excluded. Only a hint of where to look — the marker is the proof.
type Owned struct {
	Path  string `json:"path"`
	Bytes *int64 `json:"bytes,omitempty"` // measured when excluded; nil if unknown
}

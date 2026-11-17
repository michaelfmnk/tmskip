package state

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// Version is the state file format this tmskip writes.
const Version = 1

// Paths are tmskip's state and log locations under $HOME.
type Paths struct {
	Dir, File, Lock       string // ~/Library/Application Support/tmskip/…
	LogDir, Log, AgentLog string // ~/Library/Logs/tmskip/…
}

// PathsFor returns the locations for home.
func PathsFor(home string) Paths {
	dir := filepath.Join(home, "Library", "Application Support", "tmskip")
	logs := filepath.Join(home, "Library", "Logs", "tmskip")
	return Paths{
		Dir:      dir,
		File:     filepath.Join(dir, "state.json"),
		Lock:     filepath.Join(dir, "lock"),
		LogDir:   logs,
		Log:      filepath.Join(logs, "tmskip.log"),
		AgentLog: filepath.Join(logs, "agent.log"),
	}
}

// State is state.json.
type State struct {
	Version       int        `json:"version"`
	Owned         []Owned    `json:"owned"`
	LastRun       *Run       `json:"last_run,omitempty"`
	LastScheduled *Scheduled `json:"last_scheduled,omitempty"`
	Notified      []string   `json:"notified,omitempty"`
}

// Run is the result of the last sync (manual or scheduled).
type Run struct {
	At        time.Time    `json:"at"`
	Scheduled bool         `json:"scheduled"`
	ExitCode  int          `json:"exit_code"`
	Excluded  int          `json:"excluded"`
	Included  int          `json:"included"`
	Unchanged int          `json:"unchanged"`
	Problems  []RunProblem `json:"problems"`
	Error     string       `json:"error"`
}

// RunProblem is a folder the run couldn't read.
type RunProblem struct {
	Path   string `json:"path"`
	Error  string `json:"error"`
	Denied bool   `json:"denied"`
}

// Scheduled is what the last scheduled run learned about Full Disk Access:
// "granted", "denied" or "unknown".
type Scheduled struct {
	At  time.Time `json:"at"`
	FDA string    `json:"fda"`
}

// Load reads state.json. A missing file is an empty state; a corrupt one is
// an empty state plus a warning; a newer version is an error.
func Load(fs fsys.FS, p Paths) (*State, string, error) {
	empty := &State{Version: Version}
	data, err := fs.ReadFile(p.File)
	if err != nil {
		if fsys.IsNotExist(err) {
			return empty, "", nil
		}
		return nil, "", err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return empty, "state unreadable, starting fresh (markers are still the proof)", nil
	}
	if s.Version > Version {
		return nil, "", errors.New("state written by a newer tmskip — upgrade")
	}
	s.Version = Version
	return &s, "", nil
}

// Save writes state.json atomically.
func Save(fs fsys.FS, p Paths, s *State) error {
	s.Version = Version
	if s.Owned == nil {
		s.Owned = []Owned{}
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := fs.MkdirAll(p.Dir, 0o755); err != nil {
		return err
	}
	return fs.WriteFileAtomic(p.File, append(data, '\n'), 0o644)
}

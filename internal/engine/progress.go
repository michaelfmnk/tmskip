package engine

// Progress receives progress events from long phases (the walk, size
// measurement). The engine only reports; rendering and throttling live in
// the CLI. A nil Progress means no reporting.
type Progress interface {
	// Scanning: dir was just listed; folders is how many folders have been
	// listed so far, found how many are to be excluded so far.
	Scanning(dir string, folders, found int)
	// Measured: done of total paths have been measured.
	Measured(done, total int)
}

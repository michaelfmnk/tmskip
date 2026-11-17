package engine

import (
	"path"
	"sync"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// Size is the disk space used by p and everything under it: allocated
// blocks, symlinks never followed, hard-linked files counted once.
// Unreadable subfolders are skipped (best effort); only an error on p
// itself is returned.
func Size(fs fsys.FS, p string) (int64, error) {
	info, err := fs.Lstat(p)
	if err != nil {
		return 0, err
	}
	type inode struct{ dev, ino uint64 }
	seen := map[inode]bool{}
	var total int64
	add := func(i fsys.Info) {
		if i.Nlink > 1 && i.Type != fsys.TypeDir {
			k := inode{i.Dev, i.Ino}
			if seen[k] {
				return
			}
			seen[k] = true
		}
		total += i.Blocks * 512
	}
	add(info)
	if info.Type != fsys.TypeDir {
		return total, nil
	}
	var walk func(dir string)
	walk = func(dir string) {
		es, err := fs.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range es {
			c := path.Join(dir, e.Name)
			ci, err := fs.Lstat(c)
			if err != nil {
				continue
			}
			add(ci)
			if ci.Type == fsys.TypeDir {
				walk(c)
			}
		}
	}
	walk(p)
	return total, nil
}

// Sizes measures paths concurrently. Paths that can't be measured are
// absent from the result.
func Sizes(fs fsys.FS, paths []string, workers int) map[string]int64 {
	return SizesWith(fs, paths, workers, nil)
}

// SizesWith is Sizes reporting each measured path to p (which may be nil).
func SizesWith(fs fsys.FS, paths []string, workers int, p Progress) map[string]int64 {
	if workers < 1 {
		workers = 1
	}
	out := make(map[string]int64, len(paths))
	var mu sync.Mutex
	var wg sync.WaitGroup
	done := 0
	jobs := make(chan string)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				n, err := Size(fs, path)
				mu.Lock()
				if err == nil {
					out[path] = n
				}
				done++
				if p != nil {
					p.Measured(done, len(paths))
				}
				mu.Unlock()
			}
		}()
	}
	for _, path := range paths {
		jobs <- path
	}
	close(jobs)
	wg.Wait()
	return out
}

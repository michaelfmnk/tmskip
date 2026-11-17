package engine

import (
	"github.com/michaelfmnk/tmskip/internal/fsys"
	"github.com/michaelfmnk/tmskip/internal/state"
)

// CanonicalHints resolves each stored hint through the filesystem (a folder
// renamed in case, or reached through a symlinked ancestor, gets its
// canonical path) and dedupes them, keeping a known size. A hint that can't
// be resolved (unreadable ancestor) is kept as stored.
func CanonicalHints(fs fsys.FS, hints []state.Owned) []state.Owned {
	var out []state.Owned
	idx := map[string]int{}
	for _, h := range hints {
		p := h.Path
		if c, _, err := fsys.Canonical(fs, h.Path); err == nil {
			p = c
		}
		if i, ok := idx[p]; ok {
			if out[i].Bytes == nil {
				out[i].Bytes = h.Bytes
			}
			continue
		}
		idx[p] = len(out)
		out = append(out, state.Owned{Path: p, Bytes: h.Bytes})
	}
	return out
}

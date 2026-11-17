package state

import (
	"strings"
	"time"

	"github.com/michaelfmnk/tmskip/internal/fsys"
)

// MaxLogSize is the size at which tmskip.log is rotated to tmskip.log.1.
const MaxLogSize = 1 << 20

// AppendLog appends one line per event, each prefixed with an RFC 3339
// timestamp, rotating the log first when it is over MaxLogSize.
func AppendLog(fs fsys.FS, p Paths, now time.Time, lines ...string) error {
	if err := fs.MkdirAll(p.LogDir, 0o755); err != nil {
		return err
	}
	if info, err := fs.Lstat(p.Log); err == nil && info.Size > MaxLogSize {
		if err := fs.Rename(p.Log, p.Log+".1"); err != nil {
			return err
		}
	}
	var b strings.Builder
	ts := now.Format(time.RFC3339)
	for _, l := range lines {
		b.WriteString(ts + " " + l + "\n")
	}
	return fs.AppendFile(p.Log, []byte(b.String()))
}

package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/michaelfmnk/tmskip/internal/engine"
)

// progressEvery throttles redraws to at most 10 per second.
const progressEvery = 100 * time.Millisecond

// progressLine renders engine progress as one stderr line, redrawn in place
// (\r + clear to end of line), truncated to the terminal width so it never
// wraps, and cleared when a phase ends. A nil *progressLine draws nothing.
type progressLine struct {
	w     io.Writer
	now   func() time.Time
	width int    // columns; 0 = unknown (80)
	home  string // shown as ~
	last  time.Time
	shown bool
}

// progress returns the renderer, or nil when it must stay quiet: -q,
// --json, a scheduled run, or a stderr that isn't a terminal.
func (a *app) progress() *progressLine {
	env := a.d.Env
	if a.g.quiet || a.g.json || env.Scheduled() || !env.StderrTTY || env.Now == nil {
		return nil
	}
	width := 0
	if env.TermWidth != nil {
		width = env.TermWidth()
	}
	return &progressLine{w: env.Stderr, now: env.Now, width: width, home: env.Home}
}

// engine returns p as an engine.Progress (a nil interface when p is nil).
func (p *progressLine) engine() engine.Progress {
	if p == nil {
		return nil
	}
	return p
}

func (p *progressLine) Scanning(dir string, folders, found int) {
	if p == nil {
		return
	}
	p.draw(fmt.Sprintf("Scanning %s  %d folders · %d found", p.short(dir), folders, found))
}

func (p *progressLine) Measured(done, total int) {
	if p == nil {
		return
	}
	p.draw(fmt.Sprintf("Measuring sizes  %d/%d", done, total))
}

// Clear removes the line, if one is shown.
func (p *progressLine) Clear() {
	if p == nil || !p.shown {
		return
	}
	io.WriteString(p.w, "\r\x1b[K")
	p.shown = false
}

func (p *progressLine) draw(s string) {
	now := p.now()
	if p.shown && now.Sub(p.last) < progressEvery {
		return
	}
	width := p.width
	if width <= 0 {
		width = 80
	}
	if max := width - 1; utf8.RuneCountInString(s) > max { // the last column would wrap on some terminals
		r := []rune(s)
		s = string(r[:max-1]) + "…"
	}
	io.WriteString(p.w, "\r"+s+"\x1b[K")
	p.last, p.shown = now, true
}

func (p *progressLine) short(dir string) string {
	if p.home != "" && (dir == p.home || strings.HasPrefix(dir, p.home+"/")) {
		return "~" + dir[len(p.home):]
	}
	return dir
}

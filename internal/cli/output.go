package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/michaelfmnk/tmskip/internal/sysenv"
)

// output is the only thing that writes to the terminal. It honours --json,
// -q, -v, NO_COLOR, the TTY and scheduled runs.
type output struct {
	stdout, stderr io.Writer
	json           bool
	quiet          bool
	verbose        bool
	color          bool
	scheduled      bool
	hint           string
	warnings       []string
}

func newOutput(env sysenv.Env, g *globals) *output {
	noColor := env.Getenv != nil && env.Getenv("NO_COLOR") != ""
	return &output{
		stdout:    env.Stdout,
		stderr:    env.Stderr,
		json:      g.json,
		quiet:     g.quiet,
		verbose:   g.verbose && !g.quiet,
		color:     env.StdoutTTY && !noColor,
		scheduled: env.Scheduled(),
	}
}

// text reports whether human-readable stdout lines are shown.
func (o *output) text() bool { return !o.json && !o.quiet }

// Line prints a normal stdout line (not under -q or --json).
func (o *output) Line(format string, args ...any) {
	if !o.text() {
		return
	}
	fmt.Fprintf(o.stdout, format+"\n", args...)
}

// Verbose prints a stdout line only under -v.
func (o *output) Verbose(format string, args ...any) {
	if !o.text() || !o.verbose {
		return
	}
	fmt.Fprintf(o.stdout, format+"\n", args...)
}

// Warn prints a warning on stderr (shown even under -q) and remembers it for
// the JSON "warnings" field.
func (o *output) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	o.warnings = append(o.warnings, msg)
	fmt.Fprintln(o.stderr, o.yellow("warning:")+" "+msg)
}

// Errorf prints an error on stderr.
func (o *output) Errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintln(o.stderr, o.red("error:")+" "+msg)
}

// Problem prints an unreadable-folder line on stderr.
func (o *output) Problem(path, msg string) {
	fmt.Fprintln(o.stderr, o.red("!")+" "+path+": "+msg)
}

// JSON prints exactly one JSON document on stdout.
func (o *output) JSON(v any) {
	enc := json.NewEncoder(o.stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// Hint sets the command's next-step hint, printed last by flushHint.
func (o *output) Hint(h string) { o.hint = h }

func (o *output) flushHint() {
	if o.hint == "" || o.json || o.quiet || o.scheduled {
		return
	}
	fmt.Fprintln(o.stdout, o.dim("→ "+o.hint))
}

func (o *output) paint(code, s string) string {
	if !o.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (o *output) green(s string) string  { return o.paint("32", s) }
func (o *output) yellow(s string) string { return o.paint("33", s) }
func (o *output) red(s string) string    { return o.paint("31", s) }
func (o *output) dim(s string) string    { return o.paint("2", s) }

// indent prefixes every line of a multi-line message.
func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}

package cli

import (
	"runtime"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/engine"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/state"
)

func (a *app) listCommand() *cobra.Command {
	var sizes bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Folders tmskip manages, with their rule",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return a.runList(sizes) },
	}
	cmd.Flags().BoolVar(&sizes, "sizes", false, "measure each folder now")
	return cmd
}

type listFolder struct {
	Path  string `json:"path"`
	Rule  string `json:"rule,omitempty"`
	State string `json:"state"`
	Bytes *int64 `json:"bytes,omitempty"`
}

// runList lists the owned-path hints (no walk) with their current state.
func (a *app) runList(measure bool) error {
	r, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	st, warn, err := state.Load(a.d.FS, a.statePaths())
	if err != nil {
		return exitWith(ExitError, err)
	}
	if warn != "" {
		a.out.Warn("%s", warn)
	}
	fs := a.d.FS
	ex := exclusion.New(fs)
	owned := engine.CanonicalHints(fs, st.Owned)
	var live map[string]int64
	if measure {
		var ps []string
		for _, o := range owned {
			ps = append(ps, o.Path)
		}
		prog := a.progress()
		live = engine.SizesWith(fs, ps, runtime.NumCPU(), prog.engine())
		prog.Clear()
	}
	folders := []listFolder{}
	for _, o := range owned {
		f := listFolder{Path: o.Path}
		if n, ok := live[o.Path]; ok {
			f.Bytes = &n
		}
		d := engine.Evaluate(r, fs, o.Path, false)
		f.Rule = d.Match.Rule
		switch d.Reason {
		case engine.ReasonMissing:
			f.State = "missing"
		case engine.ReasonUnmounted:
			f.State = "unmounted"
		default:
			marked, _ := ex.IsMarked(o.Path)
			excluded, _ := ex.IsExcluded(o.Path)
			switch {
			case !d.Exclude || !marked:
				f.State = "no_longer_matches"
			case !excluded:
				f.State = "marked_only"
			default:
				f.State = "excluded"
			}
		}
		if f.State == "missing" || f.State == "unmounted" || f.State == "no_longer_matches" {
			f.Rule = ""
		}
		folders = append(folders, f)
	}
	if len(folders) == 0 {
		a.out.Hint("nothing managed yet — preview: tmskip sync -n")
	} else {
		a.out.Hint("why one is excluded: tmskip why <path>")
	}
	if a.g.json {
		a.out.JSON(map[string]any{"folders": folders})
		return nil
	}
	if len(folders) == 0 {
		a.out.Line("tmskip manages no folders yet.")
	}
	for _, f := range folders {
		line := f.Path
		detail := f.Rule
		if f.Bytes != nil && f.State != "missing" && f.State != "unmounted" {
			if detail != "" {
				detail += ", "
			}
			detail += humanBytes(*f.Bytes)
		}
		if detail != "" {
			line += "  (" + detail + ")"
		}
		if f.State != "excluded" {
			if detail == "" {
				line += " "
			}
			line += " [" + f.State + "]"
		}
		a.out.Line("%s", line)
	}
	return nil
}

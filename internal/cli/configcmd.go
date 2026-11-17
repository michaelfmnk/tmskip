package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

func (a *app) configCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Shipped rule groups and the config file",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "defaults [group]",
		Short: "Print the shipped rule groups and mark which you have",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runConfigDefaults(args) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "add <group>...",
		Short: "Append shipped groups to the config (refuses if a name exists)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withLock(func() error { return a.runConfigAdd(args) })
		},
	})
	var yes bool
	reset := &cobra.Command{
		Use:   "reset",
		Short: "Write the default config (backs up an existing one to .bak)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.withLock(func() error { return a.runConfigReset(yes) })
		},
	}
	reset.Flags().BoolVar(&yes, "yes", false, "don't ask before replacing an existing config")
	cmd.AddCommand(reset)
	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the resolved config path and its symlink target",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runConfigPath() },
	})
	return cmd
}

func unknownGroup(name string) error {
	return errorf(ExitError, "no shipped group %q — groups: %s", name, strings.Join(config.GroupNames(), ", "))
}

func (a *app) runConfigDefaults(args []string) error {
	groups := config.Groups()
	if len(args) == 1 {
		g, ok := config.FindGroup(args[0])
		if !ok {
			return unknownGroup(args[0])
		}
		groups = []config.Group{g}
	}
	f, err := config.Load(a.d.FS, a.configPath())
	if err != nil {
		a.out.Warn("can't compare with your config: %v", firstLine(err.Error()))
	}
	for i, g := range groups {
		mark := ""
		if f != nil {
			switch config.GroupStatus(g, f) {
			case "present":
				mark = " — in your config"
			case "changed":
				mark = " — changed in your config"
			default:
				mark = " — not in your config"
			}
		}
		if i > 0 {
			a.out.Line("")
		}
		a.out.Line("# %s%s", g.Name, mark)
		a.out.Line("%s", strings.TrimSuffix(g.Text, "\n"))
	}
	a.out.Hint("add a group: tmskip config add <group>")
	return nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func (a *app) runConfigAdd(names []string) error {
	var groups []config.Group
	for _, n := range names {
		g, ok := config.FindGroup(n)
		if !ok {
			return unknownGroup(n)
		}
		groups = append(groups, g)
	}
	path := a.configPath()
	current, err := a.d.FS.ReadFile(path)
	if err != nil {
		if fsys.IsNotExist(err) {
			return exitWith(ExitConfig, &config.MissingError{Path: path})
		}
		return exitWith(ExitError, err)
	}
	out, err := config.AppendGroups(current, groups)
	if err != nil {
		var ie *config.InvalidError
		if errors.As(err, &ie) {
			return exitWith(ExitConfig, err)
		}
		return exitWith(ExitError, err)
	}
	if err := config.WriteThrough(a.d.FS, path, out); err != nil {
		return exitWith(ExitError, fmt.Errorf("writing %s: %w", path, err))
	}
	a.out.Line("Added %s to %s", strings.Join(names, ", "), path)
	a.log("config add " + strings.Join(names, " "))
	a.out.Hint("preview: tmskip sync -n")
	return nil
}

func (a *app) runConfigReset(yes bool) error {
	path := a.configPath()
	fs := a.d.FS
	template := []byte(config.Template(detectRoots(a.d.Env, fs)))
	current, err := fs.ReadFile(path)
	switch {
	case fsys.IsNotExist(err):
		if err := config.WriteThrough(fs, path, template); err != nil {
			return exitWith(ExitError, fmt.Errorf("writing %s: %w", path, err))
		}
		a.out.Line("Wrote %s", path)
	case err != nil:
		return exitWith(ExitError, err)
	default:
		target, err := config.Target(fs, path)
		if err != nil {
			return exitWith(ExitError, err)
		}
		bak := target + ".bak"
		if !yes {
			if !a.d.Env.StdinTTY {
				return errorf(ExitError, "config reset would replace %s — rerun with --yes", path)
			}
			ok, err := a.ask(fmt.Sprintf("Replace %s? A backup goes to %s", path, bak), false)
			if err != nil {
				return exitWith(ExitError, err)
			}
			if !ok {
				a.out.Line("Nothing changed.")
				a.out.Hint("check it: tmskip doctor")
				return nil
			}
		}
		mode := fsMode(fs, target)
		if err := fs.WriteFileAtomic(bak, current, mode); err != nil {
			return exitWith(ExitError, fmt.Errorf("writing %s: %w", bak, err))
		}
		if err := config.WriteThrough(fs, path, template); err != nil {
			return exitWith(ExitError, fmt.Errorf("writing %s: %w", path, err))
		}
		a.out.Line("Wrote %s (backup: %s)", path, bak)
	}
	a.log("config reset " + path)
	a.out.Hint("preview: tmskip sync -n")
	return nil
}

func fsMode(fs fsys.FS, p string) os.FileMode {
	if info, err := fs.Lstat(p); err == nil {
		return info.Mode.Perm()
	}
	return 0o644
}

func (a *app) runConfigPath() error {
	path := a.configPath()
	fs := a.d.FS
	info, err := fs.Lstat(path)
	switch {
	case err != nil:
		a.out.Line("%s (does not exist)", path)
	case info.Type == fsys.TypeSymlink:
		target, _ := config.Target(fs, path)
		suffix := ""
		if _, ok, _ := fsys.Canonical(fs, path); !ok {
			suffix = " (does not exist)"
		}
		a.out.Line("%s → %s%s", path, target, suffix)
	default:
		a.out.Line("%s", path)
	}
	a.out.Hint("check it: tmskip doctor")
	return nil
}

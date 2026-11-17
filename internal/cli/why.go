package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/michaelfmnk/tmskip/internal/config"
	"github.com/michaelfmnk/tmskip/internal/engine"
	"github.com/michaelfmnk/tmskip/internal/exclusion"
	"github.com/michaelfmnk/tmskip/internal/fsys"
)

func (a *app) whyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "why <path>",
		Short: "Which rule, gate file, keep or guardrail decides a folder",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runWhy(args[0]) },
	}
}

type whyJSON struct {
	Path        string `json:"path"`
	ExcludedNow bool   `json:"excluded_now"`
	Owner       string `json:"owner"` // tmskip | user | none
	Decision    string `json:"decision"`
	Reason      string `json:"reason"`
	Rule        string `json:"rule"`
	Key         string `json:"key"`
	Gate        string `json:"gate"`
	Guard       string `json:"guard"`
	CoveredBy   string `json:"covered_by"`
}

func (a *app) runWhy(arg string) error {
	r, _, err := a.loadConfig()
	if err != nil {
		return err
	}
	p := config.ExpandHome(arg, a.d.Env.Home)
	if !filepath.IsAbs(p) {
		p = filepath.Join(a.d.Env.Cwd, p)
	}
	canon, exists, err := fsys.Canonical(a.d.FS, filepath.Clean(p))
	if err != nil {
		return exitWith(ExitError, err)
	}
	if !exists {
		return errorf(ExitError, "%s does not exist", arg)
	}
	fs := a.d.FS
	ex := exclusion.New(fs)
	d := engine.Evaluate(r, fs, canon, true)
	excluded, errX := ex.IsExcluded(canon)
	marked, errM := ex.IsMarked(canon)
	if errX != nil || errM != nil {
		if d.Err == nil {
			d.Reason, d.Exclude = engine.ReasonUnreadable, false
			d.Err = errX
			if d.Err == nil {
				d.Err = errM
			}
		}
	}
	owner := "none"
	switch {
	case marked:
		owner = "tmskip"
	case excluded:
		owner = "user"
	}
	decision := "none"
	switch {
	case owner == "user":
	case d.Reason == engine.ReasonUnreadable || d.Reason == engine.ReasonUnmounted:
	case d.Exclude:
		decision = "exclude"
	case marked:
		decision = "include"
	}
	a.out.Hint("preview the next sync: tmskip sync -n")
	if a.g.json {
		a.out.JSON(whyJSON{Path: canon, ExcludedNow: excluded, Owner: owner, Decision: decision, Reason: d.Reason.String(),
			Rule: d.Match.Rule, Key: d.Match.Key, Gate: d.Match.Gate, Guard: d.Guard, CoveredBy: d.CoveredBy})
		return nil
	}

	a.out.Line("%s", canon)
	now := "no"
	switch owner {
	case "tmskip":
		if excluded {
			now = "yes (by tmskip)"
		} else {
			now = "no (tmskip's marker is there, the exclusion isn't)"
		}
	case "user":
		now = "yes (excluded by you, not by tmskip)"
	}
	a.out.Line("  excluded now: %s", now)
	a.out.Line("  because: %s", whyReason(d))
	a.out.Line("  next sync: %s", nextSync(d, owner, excluded, marked))
	return nil
}

func whyReason(d engine.Decision) string {
	m := d.Match
	matched := ""
	if m.Rule != "" {
		if m.Key == "paths" {
			matched = fmt.Sprintf("is a fixed path of rule %q", m.Rule)
		} else {
			matched = fmt.Sprintf("matches rule %q (%s: %s)", m.Rule, m.Key, m.Gate)
		}
	}
	switch d.Reason {
	case engine.ReasonMatched, engine.ReasonFixed:
		if d.CoveredBy != "" {
			return fmt.Sprintf("%s, and it is inside %s, which is excluded", matched, d.CoveredBy)
		}
		return matched
	case engine.ReasonBlocked:
		if matched != "" {
			return fmt.Sprintf("%s, but a guardrail stops it: it %s", matched, d.Guard)
		}
		return "guardrail: it " + d.Guard
	case engine.ReasonNotMatched:
		if d.CoveredBy != "" {
			return fmt.Sprintf("no rule matches, but it is inside %s, which is excluded", d.CoveredBy)
		}
		return "no rule matches"
	case engine.ReasonSkipped:
		return "it is inside a skipped folder (skip)"
	case engine.ReasonOutsideRoots:
		return "it is outside roots and fixed paths"
	case engine.ReasonUnreadable:
		return "it can't be read: " + errText(d.Err)
	case engine.ReasonUnmounted:
		return "its volume is not mounted"
	case engine.ReasonMissing:
		return "it does not exist"
	}
	return d.Reason.String()
}

func nextSync(d engine.Decision, owner string, excluded, marked bool) string {
	switch {
	case owner == "user":
		return "leaves it alone — excluded by you, tmskip never touches it"
	case d.Reason == engine.ReasonUnreadable || d.Reason == engine.ReasonUnmounted:
		return "leaves it as is (it can't be checked)"
	case d.CoveredBy != "" && !marked:
		return "nothing — it is already inside an excluded folder"
	case d.Exclude && marked && excluded:
		return "keeps it excluded"
	case d.Exclude:
		return "excludes it"
	case marked:
		return "re-includes it in backups"
	}
	return "leaves it in backups"
}

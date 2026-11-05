// Package reviewcli declares the reviewrules command line with go-flags and renders the README Flags
// table and the docs Config keys table.
package reviewcli

import (
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/popoffvg/reviewrules/internal/reviewsettings"
)

// ReviewFlags are the only flags; every other choice is a key of a config file.
type ReviewFlags struct {
	Config      string `long:"config"       description:"user config file; [local] is used, or [ci] when CI=true" default-mask:"~/.config/reviewrules/config.toml"`
	Repo        string `long:"repo"         description:"repository root to review; its .reviewrules/settings.toml is the repo settings"`
	Base        string `long:"base"         description:"base ref; the diff starts at merge-base(base, HEAD)"`
	Worktree    bool   `long:"worktree"     description:"review uncommitted changes too (local run before a push)"`
	JSONOut     string `long:"json-out"     description:"write the JSON report to this file, or to stdout with \"-\"; with CI=true and no flag it goes to stdout" default-mask:"none"`
	SessionDir  string `long:"session-dir"  description:"save the messages of the parent session and of each fork as JSON files in this folder; API backends only" default-mask:"none"`
	PrintConfig bool   `long:"print-config" description:"print every merged config value and the file that gave it, then exit without a review"`
}

// DefaultReviewFlags is what a run uses for the flags the command line does not set.
func DefaultReviewFlags() ReviewFlags {
	return ReviewFlags{
		Config: reviewsettings.DefaultUserConfigPath(),
		Repo:   ".",
		Base:   "origin/main",
	}
}

// ReviewCommandLine is the parser and the flags it fills.
type ReviewCommandLine struct {
	Parser *flags.Parser
	Flags  *ReviewFlags
}

// NewReviewCommandLine builds the parser. A bool flag accepts =true and =false.
func NewReviewCommandLine() (ReviewCommandLine, error) {
	reviewFlags := DefaultReviewFlags()
	parser := flags.NewNamedParser("reviewrules", flags.HelpFlag|flags.PrintErrors|flags.PassDoubleDash|flags.AllowBoolValues)
	_, err := parser.AddGroup("Review", "", &reviewFlags)
	if err != nil {
		return ReviewCommandLine{}, fmt.Errorf("review flags: %w", err)
	}
	return ReviewCommandLine{Parser: parser, Flags: &reviewFlags}, nil
}

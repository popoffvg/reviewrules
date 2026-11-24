package reviewsettings

import (
	"fmt"
	"strings"

	"github.com/popoffvg/reviewrules/internal/review"
	"github.com/popoffvg/reviewrules/internal/ruleset"
)

// RepoSettings is the repo's .reviewrules/settings.toml: the review policy the team commits.
// The toml, description, and default-text tags of a field make its ConfigKey row.
type RepoSettings struct {
	RuleFolders       []string            `toml:"rule_folders"       description:"more rule folder names, searched like .reviewrules in every directory; .reviewrules wins, then list order"`
	FailOn            string              `toml:"fail_on"            description:"lowest rule severity whose FAIL fails the run: low, medium, high"`
	FailOnBugPriority review.BugPriority  `toml:"fail_on_bug_priority" description:"lowest confirmed bug priority that fails the run: low, high, critical"`
	FailOnUnanswered  bool                `toml:"fail_on_unanswered" description:"fail the run when a rule at or above fail_on, or the hypothesis generator, gives no answer"`
	Correctness       bool                `toml:"correctness"        description:"run the hypothesis generator and one checker fork per hypothesis"`
	MaxDiffBytes      int                 `toml:"max_diff_bytes"     description:"refuse a diff larger than this"`
	RuleSets          ruleset.RuleSetList `toml:"rule_sets"          description:"shared rule sets [[rule_sets]] with name, url, ref; each adds the rules of its root .reviewrules/ as <name>/<rule id>"`
}

// DefaultRepoSettings is what a run uses with no repo settings file.
func DefaultRepoSettings() RepoSettings {
	return RepoSettings{
		FailOn:            "medium",
		FailOnBugPriority: review.BugPriorityHigh,
		Correctness:       true,
		MaxDiffBytes:      300_000,
	}
}

func (s RepoSettings) validate(path string) error {
	if !s.FailOnBugPriority.IsValid() {
		return fmt.Errorf("repo settings invalid fail_on_bug_priority %q in %s: use low, high, or critical", s.FailOnBugPriority, path)
	}
	for _, name := range s.RuleFolders {
		if name == "" || name == "." || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
			return fmt.Errorf("repo settings bad rule folder name %q in %s: it MUST be one folder name", name, path)
		}
	}
	return nil
}

package ruleset

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// LocalRuleSets turns the rule_dirs table of the user config into rule set checkouts that need no
// fetch. Each path is a folder that holds .reviewrules/config.json. A path that starts with "~/" is
// in the home folder, and any other relative path is in repoRoot. A folder with no
// .reviewrules/config.json is skipped and named in skipped, so a repo without notes still runs.
func LocalRuleSets(repoRoot string, ruleDirs map[string]string) (checkouts []RuleSetCheckout, skipped []string, err error) {
	names := make([]string, 0, len(ruleDirs))
	for name := range ruleDirs {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		err = checkRuleSetName(name)
		if err != nil {
			return nil, nil, fmt.Errorf("rule dir name invalid: rule_dirs.%s: %w", name, err)
		}
		dir, resolveErr := resolveRuleDir(repoRoot, ruleDirs[name])
		if resolveErr != nil {
			return nil, nil, fmt.Errorf("rule dir path invalid: rule_dirs.%s: %w", name, resolveErr)
		}
		_, statErr := os.Stat(filepath.Join(dir, ".reviewrules", "config.json"))
		if statErr != nil {
			skipped = append(skipped, dir)
			continue
		}
		checkouts = append(checkouts, RuleSetCheckout{
			RuleSet: RuleSet{Name: name, URL: "file://" + dir},
			Commit:  "local",
			Dir:     dir,
		})
	}
	return checkouts, skipped, nil
}

func resolveRuleDir(repoRoot string, path string) (string, error) {
	switch {
	case strings.HasPrefix(path, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, path[2:]), nil
	case filepath.IsAbs(path):
		return filepath.Clean(path), nil
	}
	return filepath.Join(repoRoot, path), nil
}

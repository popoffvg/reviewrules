// Package rulecatalog loads review rules and context docs from the rule folders of a repo (.reviewrules/ and the folders that .reviewrules/settings.toml names) and from its rule sets.
package rulecatalog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/popoffvg/reviewrules/internal/ruleset"
)

// Severity orders rule severities; a higher value is more severe.
type Severity int

// Severity values from least to most severe; SeverityUnknown is the zero value and never a rule's severity.
const (
	SeverityUnknown Severity = iota
	SeverityLow
	SeverityMedium
	SeverityHigh
)

// ParseSeverity maps a rule severity string; an empty string means medium.
func ParseSeverity(text string) (Severity, error) {
	switch strings.ToLower(text) {
	case "low":
		return SeverityLow, nil
	case "", "medium":
		return SeverityMedium, nil
	case "high":
		return SeverityHigh, nil
	}
	return SeverityUnknown, fmt.Errorf("review rule severity %q is not low, medium, or high", text)
}

func (s Severity) String() string {
	switch s {
	case SeverityLow:
		return "low"
	case SeverityMedium:
		return "medium"
	case SeverityHigh:
		return "high"
	}
	return "unknown"
}

// MarshalJSON writes the severity as its name.
func (s Severity) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// ReviewRule is one entry of a rule folder's config.json "rules". An empty Scope applies to every diff.
type ReviewRule struct {
	ID       string
	Text     string
	Scope    []string
	Severity Severity
}

// ContextDoc is one entry of a rule folder's files.json: a file that rules refer to.
type ContextDoc struct {
	Path        string
	Description string
	Scope       []string
	// RuleSet is the name of the rule set whose files.json names the doc; empty for the reviewed repo.
	RuleSet string
	// Root is the folder Path is relative to: the rule set checkout, or empty for the reviewed repo.
	Root string
}

// RuleSetInstructions is the instructions text of one rule set's config.json.
type RuleSetInstructions struct {
	RuleSet string
	Text    string
}

// RuleCatalog is everything a review reads from the rule folders and the rule sets.
type RuleCatalog struct {
	// RuleDirs are the rule folders it read, the root one first.
	RuleDirs       []string
	Instructions   string
	IgnorePatterns []string
	// RuleSetInstructions are in the order of the rule_sets list; a rule set with no instructions has no entry.
	RuleSetInstructions []RuleSetInstructions
	Rules               []ReviewRule
	Docs                []ContextDoc
}

type ruleConfigFile struct {
	Instructions   string `json:"instructions"`
	IgnorePatterns string `json:"ignorePatterns"`
	Rules          []struct {
		ID       string   `json:"id"`
		Rule     string   `json:"rule"`
		Scope    []string `json:"scope"`
		Severity string   `json:"severity"`
	} `json:"rules"`
}

type ruleFilesFile struct {
	Files []struct {
		Path        string   `json:"path"`
		Description string   `json:"description"`
		Scope       []string `json:"scope"`
	} `json:"files"`
}

// LoadReviewRules reads the rules at the repo root, in every nested folder that git tracks, then in
// each rule set. Each folder uses its first rule folder: .reviewrules, then ruleFolders in list
// order. The root needs a rule folder; only its instructions and ignorePatterns are used. Nested
// scopes are repo-relative already.
func LoadReviewRules(ctx context.Context, repoRoot string, ruleFolders []string, ruleSets []ruleset.RuleSetCheckout) (RuleCatalog, error) {
	var catalog RuleCatalog
	configDirs, err := listReviewRuleDirs(ctx, repoRoot, ruleFolderNames(ruleFolders))
	if err != nil {
		return catalog, err
	}
	catalog.RuleDirs = configDirs
	for index, dir := range configDirs {
		err = loadReviewRuleDir(&catalog, filepath.Join(repoRoot, dir), index == 0)
		if err != nil {
			return catalog, err
		}
	}
	for _, checkout := range ruleSets {
		err = loadRuleSetRules(&catalog, checkout)
		if err != nil {
			return catalog, err
		}
	}
	return catalog, nil
}

// loadRuleSetRules reads only the root .reviewrules/ of a rule set checkout. Rule ids get the
// prefix "<rule set name>/"; scopes match paths of the reviewed repo; ignorePatterns are not used.
func loadRuleSetRules(catalog *RuleCatalog, checkout ruleset.RuleSetCheckout) error {
	configDir := filepath.Join(checkout.Dir, defaultRuleFolder)
	config, err := readRuleConfigFile(configDir)
	if err != nil {
		return fmt.Errorf("rule set rules load failed: rule set %q at %s: %w", checkout.Name, checkout.Commit, err)
	}
	if config.Instructions != "" {
		catalog.RuleSetInstructions = append(catalog.RuleSetInstructions, RuleSetInstructions{RuleSet: checkout.Name, Text: config.Instructions})
	}
	err = appendReviewRules(catalog, config, checkout.Name+"/")
	if err != nil {
		return fmt.Errorf("rule set rules load failed: rule set %q: %w", checkout.Name, err)
	}
	files, err := readRuleFilesFile(configDir)
	if err != nil {
		return fmt.Errorf("rule set rules load failed: rule set %q: %w", checkout.Name, err)
	}
	for _, entry := range files.Files {
		catalog.Docs = append(catalog.Docs, ContextDoc{
			Path:        entry.Path,
			Description: entry.Description,
			Scope:       entry.Scope,
			RuleSet:     checkout.Name,
			Root:        checkout.Dir,
		})
	}
	return nil
}

// listReviewRuleDirs returns the root rule folder first, then one rule folder per nested folder, in path order.
func listReviewRuleDirs(ctx context.Context, repoRoot string, folderNames []string) ([]string, error) {
	args := []string{"-C", repoRoot, "ls-files", "--"}
	for _, name := range folderNames {
		args = append(args, name+"/config.json", ":(glob)**/"+name+"/config.json")
	}
	output, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("review rules listing failed: %w", err)
	}
	// owner folder -> rule folder names found in it
	found := map[string]map[string]bool{}
	var owners []string
	for _, path := range strings.Fields(string(output)) {
		ruleDir := filepath.Dir(path)
		owner := filepath.Dir(ruleDir)
		if found[owner] == nil {
			found[owner] = map[string]bool{}
			owners = append(owners, owner)
		}
		found[owner][filepath.Base(ruleDir)] = true
	}
	// An untracked root folder still counts: a repo can try .reviewrules before committing it.
	for _, name := range folderNames {
		_, statErr := os.Stat(filepath.Join(repoRoot, name, "config.json"))
		if statErr == nil {
			if found["."] == nil {
				found["."] = map[string]bool{}
				owners = append(owners, ".")
			}
			found["."][name] = true
		}
	}
	if found["."] == nil {
		return nil, fmt.Errorf("review rules not found: %s has no .reviewrules/config.json; rule_folders in .reviewrules/settings.toml adds other folders", repoRoot)
	}
	slices.Sort(owners)
	dirs := []string{pickRuleFolder(".", found["."], folderNames)}
	for _, owner := range owners {
		if owner != "." {
			dirs = append(dirs, pickRuleFolder(owner, found[owner], folderNames))
		}
	}
	return dirs, nil
}

func pickRuleFolder(owner string, names map[string]bool, folderNames []string) string {
	for _, name := range folderNames {
		if names[name] {
			return filepath.Join(owner, name)
		}
	}
	return ""
}

func loadReviewRuleDir(catalog *RuleCatalog, configDir string, isRoot bool) error {
	config, err := readRuleConfigFile(configDir)
	if err != nil {
		return err
	}
	if isRoot {
		catalog.Instructions = config.Instructions
		for _, pattern := range strings.Split(config.IgnorePatterns, "\n") {
			if pattern = strings.TrimSpace(pattern); pattern != "" {
				catalog.IgnorePatterns = append(catalog.IgnorePatterns, pattern)
			}
		}
	}
	err = appendReviewRules(catalog, config, "")
	if err != nil {
		return err
	}
	files, err := readRuleFilesFile(configDir)
	if err != nil {
		return err
	}
	for _, entry := range files.Files {
		catalog.Docs = append(catalog.Docs, ContextDoc{
			Path:        entry.Path,
			Description: entry.Description,
			Scope:       entry.Scope,
		})
	}
	return nil
}

func readRuleConfigFile(configDir string) (ruleConfigFile, error) {
	var config ruleConfigFile
	raw, err := os.ReadFile(filepath.Join(configDir, "config.json")) //nolint:gosec // a rule folder of the reviewed repo or of a rule set checkout
	if err != nil {
		return config, fmt.Errorf("review rules config read failed: %w", err)
	}
	err = json.Unmarshal(raw, &config)
	if err != nil {
		return config, fmt.Errorf("review rules config parse failed for %s: %w", configDir, err)
	}
	return config, nil
}

// readRuleFilesFile reads files.json; a missing file is an empty list.
func readRuleFilesFile(configDir string) (ruleFilesFile, error) {
	var files ruleFilesFile
	raw, err := os.ReadFile(filepath.Join(configDir, "files.json")) //nolint:gosec // a rule folder of the reviewed repo or of a rule set checkout
	if os.IsNotExist(err) {
		return files, nil
	}
	if err != nil {
		return files, fmt.Errorf("review rules files.json read failed: %w", err)
	}
	err = json.Unmarshal(raw, &files)
	if err != nil {
		return files, fmt.Errorf("review rules files.json parse failed for %s: %w", configDir, err)
	}
	return files, nil
}

func appendReviewRules(catalog *RuleCatalog, config ruleConfigFile, idPrefix string) error {
	for _, entry := range config.Rules {
		id := idPrefix + entry.ID
		severity, err := ParseSeverity(entry.Severity)
		if err != nil {
			return fmt.Errorf("review rule %s: %w", id, err)
		}
		catalog.Rules = append(catalog.Rules, ReviewRule{
			ID:       id,
			Text:     entry.Rule,
			Scope:    entry.Scope,
			Severity: severity,
		})
	}
	return nil
}

// ScopeMatchesAnyFile reports whether a changed file matches one of the scope globs.
// An empty scope matches every change set.
func ScopeMatchesAnyFile(scope []string, changedFiles []string) bool {
	if len(scope) == 0 {
		return true
	}
	for _, pattern := range scope {
		for _, file := range changedFiles {
			if matched, _ := doublestar.Match(pattern, file); matched {
				return true
			}
		}
	}
	return false
}

// defaultRuleFolder is the rule folder name every repo has; it also holds settings.toml.
const defaultRuleFolder = ".reviewrules"

// ruleFolderNames lists the rule folder names in the order a directory picks them: .reviewrules first.
func ruleFolderNames(ruleFolders []string) []string {
	names := []string{defaultRuleFolder}
	for _, name := range ruleFolders {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

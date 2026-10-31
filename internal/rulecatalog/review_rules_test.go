package rulecatalog

import (
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/ruleset"
)

var teamRuleFolders = []string{".team-rules"}

func TestLoadReviewRulesDetectsRuleFolder(t *testing.T) {
	repo := t.TempDir()
	writeRuleFile(t, repo, ".team-rules/config.json", `{"instructions":"root team","rules":[{"id":"root-team","rule":"g"}]}`)
	writeRuleFile(t, repo, ".reviewrules/config.json", `{"instructions":"root reviewrules","rules":[{"id":"root-reviewrules","rule":"r"}]}`)
	writeRuleFile(t, repo, "pkg/a/.team-rules/config.json", `{"rules":[{"id":"a-team","rule":"g"}]}`)
	writeRuleFile(t, repo, "pkg/b/.team-rules/config.json", `{"rules":[{"id":"b-team","rule":"g"}]}`)
	writeRuleFile(t, repo, "pkg/b/.reviewrules/config.json", `{"rules":[{"id":"b-reviewrules","rule":"r"}]}`)
	runGitInRepo(t, repo, "init", "-q")
	runGitInRepo(t, repo, "add", "-A")

	set, err := LoadReviewRules(t.Context(), repo, teamRuleFolders, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantDirs := []string{".reviewrules", "pkg/a/.team-rules", "pkg/b/.reviewrules"}
	if !slices.Equal(set.RuleDirs, wantDirs) {
		t.Errorf("rule dirs = %v, want %v", set.RuleDirs, wantDirs)
	}
	var ids []string
	for _, rule := range set.Rules {
		ids = append(ids, rule.ID)
	}
	wantIDs := []string{"root-reviewrules", "a-team", "b-reviewrules"}
	if !slices.Equal(ids, wantIDs) {
		t.Errorf("rule ids = %v, want %v", ids, wantIDs)
	}
	if set.Instructions != "root reviewrules" {
		t.Errorf("instructions = %q, want the root .reviewrules ones", set.Instructions)
	}

	t.Run("extra-folder-only-root", func(t *testing.T) {
		extraRepo := t.TempDir()
		writeRuleFile(t, extraRepo, ".team-rules/config.json", `{"instructions":"team","rules":[{"id":"only","rule":"g"}]}`)
		runGitInRepo(t, extraRepo, "init", "-q")
		only, err := LoadReviewRules(t.Context(), extraRepo, teamRuleFolders, nil)
		if err != nil || !slices.Equal(only.RuleDirs, []string{".team-rules"}) {
			t.Fatalf("rule dirs = %v, err %v; want [.team-rules]", only.RuleDirs, err)
		}
		if only.Instructions != "team" {
			t.Errorf("instructions = %q, want the root .team-rules ones", only.Instructions)
		}
	})

	t.Run("folder-order-from-rule-folders", func(t *testing.T) {
		orderRepo := t.TempDir()
		writeRuleFile(t, orderRepo, ".first/config.json", `{"rules":[{"id":"first","rule":"f"}]}`)
		writeRuleFile(t, orderRepo, ".second/config.json", `{"rules":[{"id":"second","rule":"s"}]}`)
		writeRuleFile(t, orderRepo, "pkg/.first/config.json", `{"rules":[{"id":"pkg-first","rule":"f"}]}`)
		writeRuleFile(t, orderRepo, "pkg/.second/config.json", `{"rules":[{"id":"pkg-second","rule":"s"}]}`)
		runGitInRepo(t, orderRepo, "init", "-q")
		runGitInRepo(t, orderRepo, "add", "-A")
		ordered, err := LoadReviewRules(t.Context(), orderRepo, []string{".second", ".first"}, nil)
		want := []string{".second", "pkg/.second"}
		if err != nil || !slices.Equal(ordered.RuleDirs, want) {
			t.Fatalf("rule dirs = %v, err %v; want %v", ordered.RuleDirs, err, want)
		}
	})

	t.Run("no-rule-folders-ignores-other-folders", func(t *testing.T) {
		plainRepo := t.TempDir()
		writeRuleFile(t, plainRepo, ".team-rules/config.json", `{"rules":[{"id":"team","rule":"g"}]}`)
		runGitInRepo(t, plainRepo, "init", "-q")
		_, err := LoadReviewRules(t.Context(), plainRepo, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "review rules not found") || !strings.Contains(err.Error(), "rule_folders") {
			t.Fatalf("err = %v, want review rules not found that names rule_folders", err)
		}
	})

	t.Run("no-rule-folder", func(t *testing.T) {
		empty := t.TempDir()
		runGitInRepo(t, empty, "init", "-q")
		_, err := LoadReviewRules(t.Context(), empty, nil, nil)
		if err == nil {
			t.Fatal("LoadReviewRules accepted a repo with no rule folder")
		}
	})
}

func TestRuleFolderNamesWithoutExtraFolders(t *testing.T) {
	if got := ruleFolderNames(nil); !slices.Equal(got, []string{".reviewrules"}) {
		t.Errorf("rule folder names = %v, want [.reviewrules]", got)
	}
}

func writeRuleFile(t testing.TB, repo string, path string, content string) {
	t.Helper()
	full := filepath.Join(repo, path)
	err := os.MkdirAll(filepath.Dir(full), 0o750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(full, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func runGitInRepo(t testing.TB, repo string, args ...string) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", repo}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestLoadReviewRulesMergesRuleSets(t *testing.T) {
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("rule set tests need the git binary on PATH: " + err.Error())
	}
	source := t.TempDir()
	writeRuleFile(t, source, ".reviewrules/config.json", `{
		"instructions": "Go set instructions.",
		"ignorePatterns": "vendor/**",
		"rules": [
			{"id": "errors", "rule": "Wrap errors.", "scope": ["**/*.go"], "severity": "high"},
			{"id": "test-conventions", "rule": "Tests: table tests; t.Context; no sleeps"}
		]
	}`)
	writeRuleFile(t, source, ".reviewrules/files.json", `{"files":[{"path":"docs/go.md","description":"Go style"}]}`)
	writeRuleFile(t, source, ".reviewrules/settings.toml", `rule_folders = [".other"]`)
	writeRuleFile(t, source, "nested/.reviewrules/config.json", `{"rules":[{"id":"nested","rule":"n"}]}`)
	writeRuleFile(t, source, "docs/go.md", "Go style text")
	runGitInRepo(t, source, "init", "-q")
	runGitInRepo(t, source, "add", "-A")
	runGitInRepo(t, source, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+os.DevNull, "commit", "-q", "-m", "rules")
	logger := slog.New(slog.DiscardHandler)
	checkout, err := ruleset.FetchRuleSet(t.Context(), logger, t.TempDir(), ruleset.RuleSet{Name: "go", URL: "file://" + filepath.ToSlash(source), Ref: "HEAD"})
	if err != nil {
		t.Fatal(err)
	}

	repo := t.TempDir()
	writeRuleFile(t, repo, ".reviewrules/config.json", `{"instructions":"repo","ignorePatterns":"gen/**","rules":[{"id":"own","rule":"o"}]}`)
	runGitInRepo(t, repo, "init", "-q")
	catalog, err := LoadReviewRules(t.Context(), repo, nil, []ruleset.RuleSetCheckout{checkout})
	if err != nil {
		t.Fatal(err)
	}

	var ids []string
	for _, rule := range catalog.Rules {
		for _, requirement := range SplitRuleRequirements(rule) {
			ids = append(ids, requirement.ID)
		}
	}
	wantIDs := []string{"own", "go/errors", "go/test-conventions#1", "go/test-conventions#2", "go/test-conventions#3"}
	if !slices.Equal(ids, wantIDs) {
		t.Errorf("rule ids = %v, want %v", ids, wantIDs)
	}
	if catalog.Rules[1].Severity != SeverityHigh || !slices.Equal(catalog.Rules[1].Scope, []string{"**/*.go"}) {
		t.Errorf("go/errors = %+v; want severity high and its scope", catalog.Rules[1])
	}
	if catalog.Instructions != "repo" || !slices.Equal(catalog.IgnorePatterns, []string{"gen/**"}) {
		t.Errorf("instructions %q, ignore patterns %v; want the repo's own only", catalog.Instructions, catalog.IgnorePatterns)
	}
	wantInstructions := []RuleSetInstructions{{RuleSet: "go", Text: "Go set instructions."}}
	if !slices.Equal(catalog.RuleSetInstructions, wantInstructions) {
		t.Errorf("rule set instructions = %+v, want %+v", catalog.RuleSetInstructions, wantInstructions)
	}
	if len(catalog.Docs) != 1 || catalog.Docs[0].RuleSet != "go" || catalog.Docs[0].Root != checkout.Dir || catalog.Docs[0].Path != "docs/go.md" {
		t.Errorf("docs = %+v; want docs/go.md from the rule set checkout", catalog.Docs)
	}
}

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

func TestChooseActionsTakesTheFirstMatchingProfileFromTheTop(t *testing.T) {
	// The setup from docs/backends.md: Sonnet rules on a large diff, Haiku below, Opus hypotheses.
	profiles := []ReviewProfile{
		{If: "diff_bytes >= 65536", Action: ProfileActionRules, Model: "sonnet"},
		{Action: ProfileActionRules, Model: "haiku"},
		{Action: ProfileActionHypothesis, Model: "opus", ReasoningEffort: "medium"},
	}
	base := ReviewRunConfig{ModelName: "case-model", ReasoningEffort: "low", RuleReasoningEffort: "high", Profiles: profiles}
	cases := []struct {
		name string
		cfg  ReviewRunConfig
		diff ProfileDiff
		want reviewChoices
	}{
		{"large-diff-matches-the-first-rules-profile", base, ProfileDiff{DiffBytes: 65_536}, reviewChoices{
			Rules:           ForkSettings{Model: "sonnet", ReasoningEffort: "high"},
			Hypothesis:      ForkSettings{Model: "opus", ReasoningEffort: "medium"},
			HypothesisCheck: ForkSettings{Model: "sonnet", ReasoningEffort: "low"},
		}},
		{"small-diff-falls-to-the-next-rules-profile", base, ProfileDiff{DiffBytes: 65_535}, reviewChoices{
			Rules:           ForkSettings{Model: "haiku", ReasoningEffort: "high"},
			Hypothesis:      ForkSettings{Model: "opus", ReasoningEffort: "medium"},
			HypothesisCheck: ForkSettings{Model: "haiku", ReasoningEffort: "low"},
		}},
		{"no-profile-keeps-the-case-keys", ReviewRunConfig{ModelName: "m", HypothesisModelName: "h", ReasoningEffort: "low"}, ProfileDiff{}, reviewChoices{
			Rules:           ForkSettings{Model: "m", ReasoningEffort: "low"},
			Hypothesis:      ForkSettings{Model: "h", ReasoningEffort: "low"},
			HypothesisCheck: ForkSettings{Model: "m", ReasoningEffort: "low"},
		}},
		{"changed-files-is-a-variable", ReviewRunConfig{ModelName: "m", Profiles: []ReviewProfile{
			{If: "changed_files > 10 && diff_bytes < 1000", Action: ProfileActionHypothesisCheck, Model: "c"},
		}}, ProfileDiff{ChangedFiles: 11, DiffBytes: 999}, reviewChoices{
			Rules: ForkSettings{Model: "m"}, Hypothesis: ForkSettings{Model: "m"}, HypothesisCheck: ForkSettings{Model: "c"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.cfg.chooseActions(tc.diff)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("chooseActions = %+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestChooseActionsRejectsABadProfile(t *testing.T) {
	cases := []struct {
		name    string
		profile ReviewProfile
		want    string
	}{
		{"unknown-action", ReviewProfile{Action: "parent", Model: "m"}, `action "parent" is not one of`},
		{"empty-model", ReviewProfile{Action: ProfileActionRules}, "model is empty"},
		{"unknown-variable", ReviewProfile{If: "lines > 10", Action: ProfileActionRules, Model: "m"}, `if "lines > 10"`},
		{"not-a-condition", ReviewProfile{If: "diff_bytes + 1", Action: ProfileActionRules, Model: "m"}, `if "diff_bytes + 1"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ReviewRunConfig{ModelName: "m", Profiles: []ReviewProfile{{Action: ProfileActionRules, Model: "ok"}, tc.profile}}
			_, err := cfg.chooseActions(ProfileDiff{})
			if err == nil || !strings.Contains(err.Error(), "review profile 2: ") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v; want review profile 2 and %q", err, tc.want)
			}
		})
	}
}

func TestLoadScopedContextDocsReadsRuleSetCheckout(t *testing.T) {
	repo := t.TempDir()
	checkout := t.TempDir()
	err := os.MkdirAll(filepath.Join(checkout, "docs"), 0o750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(checkout, "docs", "go.md"), []byte("set text"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	docs, err := loadScopedContextDocs(repo, []rulecatalog.ContextDoc{{Path: "docs/go.md", Description: "Go", RuleSet: "go", Root: checkout}}, []string{"a.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Content != "set text" || docs[0].Path != "docs/go.md (rule set go)" {
		t.Fatalf("docs = %+v; want the checkout's docs/go.md named with its rule set", docs)
	}

	_, err = loadScopedContextDocs(repo, []rulecatalog.ContextDoc{{Path: "../outside.md", RuleSet: "go", Root: checkout}}, []string{"a.go"})
	if err == nil || !strings.Contains(err.Error(), "context doc ../outside.md (rule set go) read failed") {
		t.Fatalf("err = %v; want a refused path outside the checkout", err)
	}
}

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

func TestReviewModelsSwitchAtLargeDiffBytes(t *testing.T) {
	cases := []struct {
		name                     string
		cfg                      ReviewRunConfig
		diffBytes                int
		model, hypothesis, check string
	}{
		{"small-diff-keeps-model", ReviewRunConfig{ModelName: "haiku", LargeDiffModel: "sonnet", LargeDiffBytes: 100},
			99, "haiku", "haiku", "haiku"},
		{"diff-at-the-limit-uses-the-large-model", ReviewRunConfig{ModelName: "haiku", LargeDiffModel: "sonnet", LargeDiffBytes: 100},
			100, "sonnet", "sonnet", "sonnet"},
		{"set-hypothesis-models-stay", ReviewRunConfig{
			ModelName: "haiku", LargeDiffModel: "sonnet", LargeDiffBytes: 100, HypothesisModelName: "opus", HypothesisCheckModelName: "opus",
		}, 100, "sonnet", "opus", "opus"},
		{"no-large-model-keeps-model", ReviewRunConfig{ModelName: "haiku", LargeDiffBytes: 100},
			1_000, "haiku", "haiku", "haiku"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model, hypothesis, check := tc.cfg.reviewModels(tc.diffBytes)
			if model != tc.model || hypothesis != tc.hypothesis || check != tc.check {
				t.Errorf("reviewModels = %s, %s, %s; want %s, %s, %s", model, hypothesis, check, tc.model, tc.hypothesis, tc.check)
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

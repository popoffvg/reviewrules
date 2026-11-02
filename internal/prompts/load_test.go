package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEmptyDirKeepsEmbeddedPrompts(t *testing.T) {
	set, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if set.ReviewSystem != ReviewSystem || set.HypothesisGenerator != HypothesisGenerator {
		t.Fatal("empty prompts_dir changed an embedded prompt")
	}
	got := set.RuleCheck(RuleCheckInput{ID: "r1", Severity: "high", Text: "do the thing"})
	if !strings.Contains(got, "Rule id: r1") || !strings.Contains(got, "do the thing") {
		t.Fatalf("rule prompt = %q; want the rule id and text", got)
	}
}

func TestLoadReplacesOneFile(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, FileReviewSystem), []byte("custom system\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	set, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if set.ReviewSystem != "custom system" {
		t.Fatalf("system prompt = %q; want the file", set.ReviewSystem)
	}
	if set.JSONOnlyRetry != JSONOnlyRetry || set.LastStepAnswer != LastStepAnswer {
		t.Fatal("a replaced system prompt changed another prompt")
	}
}

func TestLoadRejectsUnknownEmptyAndBrokenFiles(t *testing.T) {
	t.Run("unknown", func(t *testing.T) {
		dir := t.TempDir()
		writePromptFile(t, dir, "notes.md", "hello")
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "unknown prompt file") {
			t.Fatalf("error = %v; want unknown prompt file", err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		dir := t.TempDir()
		writePromptFile(t, dir, FileJSONOnlyRetry, " \n")
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), "is empty") {
			t.Fatalf("error = %v; want empty", err)
		}
	})
	t.Run("broken-template", func(t *testing.T) {
		dir := t.TempDir()
		writePromptFile(t, dir, FileRuleCheck, "rule {{.Missing")
		_, err := Load(dir)
		if err == nil || !strings.Contains(err.Error(), FileRuleCheck) {
			t.Fatalf("error = %v; want the template name", err)
		}
	})
	t.Run("missing-dir", func(t *testing.T) {
		_, err := Load(filepath.Join(t.TempDir(), "absent"))
		if err == nil || !strings.Contains(err.Error(), "prompts_dir") {
			t.Fatalf("error = %v; want prompts_dir", err)
		}
	})
}

func writePromptFile(t *testing.T, dir string, name string, text string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

package prompts

import (
	"strings"
	"testing"
)

func TestParentSessionWithoutRuleSets(t *testing.T) {
	got := ParentSession(ParentSessionInput{
		Instructions: "Repo instructions.",
		Docs:         []ConventionDoc{{Path: "docs/a.md", Description: "A", Content: "a text"}},
		Base:         "main",
		ChangedFiles: []string{"a.go"},
		Patch:        "+x",
	})
	want := "# Repository review instructions\n\nRepo instructions.\n\n# Convention docs the review rules refer to\n\n" +
		"## docs/a.md — A\n\na text\n\n# The diff under review (base main)\n\nChanged files:\n- a.go\n\n```diff\n+x\n```\n\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("parent prompt without rule sets changed:\n%q\nwant prefix:\n%q", got, want)
	}
}

func TestParentSessionRuleSetInstructions(t *testing.T) {
	got := ParentSession(ParentSessionInput{
		Instructions: "Repo instructions.",
		RuleSetInstructions: []RuleSetInstructions{
			{Name: "go", Text: "Go instructions."},
			{Name: "sql", Text: "SQL instructions."},
		},
		Base: "main",
	})
	want := "# Repository review instructions\n\nRepo instructions.\n\n# Rule set go review instructions\n\nGo instructions.\n\n" +
		"# Rule set sql review instructions\n\nSQL instructions.\n\n# Convention docs the review rules refer to\n"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("parent prompt =\n%q\nwant prefix:\n%q", got, want)
	}
}

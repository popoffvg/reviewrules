package rulecatalog

import "testing"

func TestSplitRuleRequirements(t *testing.T) {
	rule := ReviewRule{
		ID:   "test-conventions",
		Text: "Test changes must follow .claude/rules/testing.md: testify; t.Cleanup over manual teardown (e.g. a deferred Close); pltest.Logging (never zaptest.NewLogger; it writes to stdout); t.Run titles use '-' not spaces.",
	}
	got := SplitRuleRequirements(rule)
	want := []string{
		"Test changes must follow .claude/rules/testing.md: testify.",
		"Test changes must follow .claude/rules/testing.md: t.Cleanup over manual teardown (e.g. a deferred Close).",
		"Test changes must follow .claude/rules/testing.md: pltest.Logging (never zaptest.NewLogger; it writes to stdout).",
		"Test changes must follow .claude/rules/testing.md: t.Run titles use '-' not spaces.",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d requirements, want %d: %+v", len(got), len(want), got)
	}
	for index := range want {
		if got[index].Text != want[index] {
			t.Errorf("requirement %d text = %q, want %q", index+1, got[index].Text, want[index])
		}
	}
	if got[1].ID != "test-conventions#2" {
		t.Errorf("requirement 2 id = %q, want test-conventions#2", got[1].ID)
	}

	unchanged := []ReviewRule{
		{ID: "no-list", Text: "A nil error guarantees a non-nil pointer. Flag violations."},
		{ID: "two-clauses", Text: "Bodies are read by executives: Added is present tense; Fixed is past tense."},
		{ID: "lead-is-prose", Text: "Retiring a flag takes two releases. The first PR: deprecates it; keeps the field; adds a warning."},
		{ID: "long-item", Text: "Kind must match intent: new -> Added; fix -> Fixed; security -> Security. " +
			"Kind drives the version bump, so flag a kind the diff contradicts, and read the changelog skill for the full table of kinds and their bumps."},
	}
	for _, rule := range unchanged {
		t.Run("unchanged-"+rule.ID, func(t *testing.T) {
			split := SplitRuleRequirements(rule)
			if len(split) != 1 || split[0].ID != rule.ID || split[0].Text != rule.Text {
				t.Fatalf("rule was split but is not a checklist: %+v", split)
			}
		})
	}
}

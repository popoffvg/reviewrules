package rulecatalog

import (
	"strings"
	"testing"
)

func TestParseThinkingProfile(t *testing.T) {
	tests := []struct {
		text    string
		want    ThinkingProfile
		wantErr string
	}{
		{text: "", want: ThinkingProfileMedium},
		{text: "light", want: ThinkingProfileLight},
		{text: "Heavy", want: ThinkingProfileHeavy},
		{text: "deep", wantErr: `review rule profile "deep" is not light, medium, or heavy`},
	}
	for _, tt := range tests {
		t.Run("profile-"+tt.text, func(t *testing.T) {
			got, err := ParseThinkingProfile(tt.text)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v; want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("profile = %q, err %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestLoadReviewRulesReadsProfile(t *testing.T) {
	repo := t.TempDir()
	writeRuleFile(t, repo, ".reviewrules/config.json", `{"rules":[{"id":"cheap","rule":"r","profile":"light"},{"id":"plain","rule":"r"},{"id":"list","rule":"lead: a; b; c","profile":"heavy"}]}`)
	runGitInRepo(t, repo, "init", "-q")
	catalog, err := LoadReviewRules(t.Context(), repo, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []ThinkingProfile{ThinkingProfileLight, ThinkingProfileMedium, ThinkingProfileHeavy}
	for index, rule := range catalog.Rules {
		if rule.Profile != want[index] {
			t.Errorf("rule %s profile = %q; want %q", rule.ID, rule.Profile, want[index])
		}
	}
	for _, requirement := range SplitRuleRequirements(catalog.Rules[2]) {
		if requirement.Profile != ThinkingProfileHeavy {
			t.Errorf("requirement %s profile = %q; want heavy", requirement.ID, requirement.Profile)
		}
	}

	writeRuleFile(t, repo, ".reviewrules/config.json", `{"rules":[{"id":"bad","rule":"r","profile":"deep"}]}`)
	_, err = LoadReviewRules(t.Context(), repo, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "review rule bad: review rule profile") {
		t.Fatalf("err = %v; want the bad profile named", err)
	}
}

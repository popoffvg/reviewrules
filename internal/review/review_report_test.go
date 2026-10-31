package review

import (
	"encoding/json"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

func TestReviewFailed(t *testing.T) {
	rule := func(verdict RuleVerdict, severity rulecatalog.Severity) RuleCheckResult {
		return RuleCheckResult{RuleID: "r", Verdict: verdict, Severity: severity}
	}
	cases := []struct {
		name             string
		report           ReviewReport
		failOnUnanswered bool
		want             bool
	}{
		{"all-pass", ReviewReport{Rules: []RuleCheckResult{rule(RuleVerdictPass, rulecatalog.SeverityHigh)}}, false, false},
		{"fail-at-bar", ReviewReport{Rules: []RuleCheckResult{rule(RuleVerdictFail, rulecatalog.SeverityMedium)}}, false, true},
		{"fail-below-bar", ReviewReport{Rules: []RuleCheckResult{rule(RuleVerdictFail, rulecatalog.SeverityLow)}}, false, false},
		{"error-is-warning", ReviewReport{Rules: []RuleCheckResult{rule(RuleVerdictError, rulecatalog.SeverityHigh)}}, false, false},
		{"error-fails-with-flag", ReviewReport{Rules: []RuleCheckResult{rule(RuleVerdictError, rulecatalog.SeverityHigh)}}, true, true},
		{"error-below-bar-with-flag", ReviewReport{Rules: []RuleCheckResult{rule(RuleVerdictError, rulecatalog.SeverityLow)}}, true, false},
		{"generator-error-is-warning", ReviewReport{Correctness: CorrectnessReport{Enabled: true, GeneratorError: "no JSON"}}, false, false},
		{"generator-error-fails-with-flag", ReviewReport{Correctness: CorrectnessReport{Enabled: true, GeneratorError: "no JSON"}}, true, true},
		{"confirmed-hypothesis", ReviewReport{Correctness: CorrectnessReport{
			Enabled:         true,
			HypothesisCheck: []HypothesisCheck{{Verdict: HypothesisVerdictConfirmed}},
		}}, false, true},
		{"unsure-hypothesis", ReviewReport{Correctness: CorrectnessReport{
			Enabled:         true,
			HypothesisCheck: []HypothesisCheck{{Verdict: HypothesisVerdictUnsure}},
		}}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.report.ReviewFailed(rulecatalog.SeverityMedium, tc.failOnUnanswered)
			if got != tc.want {
				t.Fatalf("ReviewFailed = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReviewFailureReasonsNameTheHypothesis(t *testing.T) {
	report := ReviewReport{Correctness: CorrectnessReport{
		Enabled: true,
		HypothesisCheck: []HypothesisCheck{
			{Hypothesis: Hypothesis{ID: "H2", At: "lease.go:84", Question: "Does cleanup release another writer's lease?"}, Verdict: HypothesisVerdictConfirmed, Scenario: "lease taken over before Stat"},
			{Hypothesis: Hypothesis{ID: "H3", At: "lease.go:90", Question: "refuted one"}, Verdict: HypothesisVerdictRefuted},
		},
	}}
	reasons := report.ReviewFailureReasons(rulecatalog.SeverityMedium, false)
	if len(reasons) != 1 {
		t.Fatalf("reasons = %v, want one for H2", reasons)
	}
	for _, part := range []string{"H2", "lease.go:84", "Does cleanup release another writer's lease?", "lease taken over before Stat"} {
		if !strings.Contains(reasons[0], part) {
			t.Errorf("reason %q does not name %q", reasons[0], part)
		}
	}
}

func TestReviewReportJSONSplitsCorrectnessUsage(t *testing.T) {
	report := ReviewReport{
		Model:       "small",
		RulesUsage:  fantasy.Usage{InputTokens: 1},
		Correctness: CorrectnessReport{Usage: fantasy.Usage{InputTokens: 2}},
	}
	report.Cost = EstimateReviewCost(report, map[string]ModelTokenPrice{"small": {Input: 1}})
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"rules_usage":`, `"check_usage":`, `"per_part_usd":{"rules":`, `"part":"rules"`} {
		if !strings.Contains(string(encoded), field) {
			t.Errorf("report JSON has no %s: %s", field, encoded)
		}
	}
}

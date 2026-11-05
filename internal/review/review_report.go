package review

import (
	"fmt"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

// ReviewReport is the JSON a run writes. Rules skipped by scope are listed by id, not checked.
type ReviewReport struct {
	Base                 string `json:"base"`
	Model                string `json:"model"`
	HypothesisModel      string `json:"hypothesis_model"`
	HypothesisCheckModel string `json:"hypothesis_check_model"`
	// RuleDirs are the rule folders the run read, the root one first.
	RuleDirs       []string          `json:"rule_dirs"`
	ChangedFiles   []string          `json:"changed_files"`
	SkippedRuleIDs []string          `json:"skipped_rule_ids"`
	Rules          []RuleCheckResult `json:"rules"`
	Correctness    CorrectnessReport `json:"correctness"`
	ParentUsage    fantasy.Usage     `json:"parent_usage"`
	// ParentRequests is the usage of each model request of the parent session, when the backend reports them.
	ParentRequests []fantasy.Usage    `json:"parent_requests,omitempty"`
	RulesUsage     fantasy.Usage      `json:"rules_usage"`
	TotalUsage     fantasy.Usage      `json:"total_usage"`
	Cost           ReviewCostEstimate `json:"cost_estimate"`
}

// CorrectnessReport holds the generator outcome and one check per hypothesis.
type CorrectnessReport struct {
	Enabled           bool            `json:"enabled"`
	GeneratorError    string          `json:"generator_error,omitempty"`
	GeneratorUsage    fantasy.Usage   `json:"generator_usage"`
	GeneratorRequests []fantasy.Usage `json:"generator_requests,omitempty"`
	CheckUsage        fantasy.Usage   `json:"check_usage"`
	// Usage is GeneratorUsage plus CheckUsage.
	Usage           fantasy.Usage     `json:"usage"`
	HypothesisCheck []HypothesisCheck `json:"hypotheses"`
}

// ReviewFailed reports whether the run must fail CI; ReviewFailureReasons says why.
func (r ReviewReport) ReviewFailed(failOn rulecatalog.Severity, failOnUnanswered bool) bool {
	return len(r.ReviewFailureReasons(failOn, failOnUnanswered)) > 0
}

// ReviewFailureReasons lists one line per check that fails the run: a FAIL rule at or above
// failOn, or a CONFIRMED hypothesis with its question and
// the input that breaks the code. An unanswered check (an ERROR rule or a generator error) is a
// warning, because a cheap model sometimes skips the answer format; failOnUnanswered makes it
// fail under the same severity bar as a FAIL.
func (r ReviewReport) ReviewFailureReasons(failOn rulecatalog.Severity, failOnUnanswered bool) []string {
	var reasons []string
	for _, rule := range r.Rules {
		if rule.Severity < failOn {
			continue
		}
		if rule.Verdict == RuleVerdictFail {
			reasons = append(reasons, fmt.Sprintf("rule %s (%s) failed with %d finding(s)", rule.RuleID, rule.Severity, len(rule.Findings)))
		}
		if rule.Verdict == RuleVerdictError && failOnUnanswered {
			reasons = append(reasons, fmt.Sprintf("rule %s (%s) gave no answer: %s", rule.RuleID, rule.Severity, rule.Error))
		}
	}
	if failOnUnanswered && r.Correctness.Enabled && r.Correctness.GeneratorError != "" {
		reasons = append(reasons, "hypothesis generator gave no answer: "+r.Correctness.GeneratorError)
	}
	for _, check := range r.Correctness.HypothesisCheck {
		if check.Verdict == HypothesisVerdictConfirmed {
			reasons = append(reasons, fmt.Sprintf("hypothesis %s at %s is confirmed: %s Input that breaks it: %s", check.ID, check.At, check.Question, check.Scenario))
		}
	}
	return reasons
}

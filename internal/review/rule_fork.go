package review

import (
	"context"
	"errors"
	"strings"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/prompts"
	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

// RuleVerdict is a rule fork's answer. RuleVerdictError means the fork failed or gave no JSON verdict.
type RuleVerdict string

// RuleVerdict values; RuleVerdictUnknown is the zero value and never a fork's answer.
const (
	RuleVerdictUnknown RuleVerdict = ""
	RuleVerdictPass    RuleVerdict = "PASS"
	RuleVerdictFail    RuleVerdict = "FAIL"
	RuleVerdictError   RuleVerdict = "ERROR"
)

// RuleFinding is one violation a rule fork reported.
type RuleFinding struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// RuleCheckResult is the outcome of one rule fork.
type RuleCheckResult struct {
	RuleID   string               `json:"rule_id"`
	Severity rulecatalog.Severity `json:"severity"`
	Verdict  RuleVerdict          `json:"verdict"`
	Findings []RuleFinding        `json:"findings"`
	Checks   []RequirementCheck   `json:"checks"`
	Error    string               `json:"error,omitempty"`
	RawReply string               `json:"raw_reply,omitempty"`
	Usage    fantasy.Usage        `json:"usage"`
}

// RequirementCheck is the fork's answer for one atomic requirement of a rule.
type RequirementCheck struct {
	Requirement string `json:"requirement"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence"`
}

type ruleForkReply struct {
	Checks   []RequirementCheck `json:"checks"`
	Verdict  RuleVerdict        `json:"verdict"`
	Findings []RuleFinding      `json:"findings"`
}

// CheckRuleInFork forks the parent session and checks the diff against one rule.
func CheckRuleInFork(
	ctx context.Context,
	agent ReviewForkAgent,
	settings ForkSettings,
	rule rulecatalog.ReviewRule,
	text prompts.Set,
) RuleCheckResult {
	check := RuleCheckResult{
		RuleID:   rule.ID,
		Severity: rule.Severity,
		Verdict:  RuleVerdictError,
		Findings: []RuleFinding{},
	}
	prompt := text.RuleCheck(prompts.RuleCheckInput{ID: rule.ID, Severity: rule.Severity.String(), Text: rule.Text})
	fork, err := agent.ForkParentSession(ctx, prompt, settings)
	if err != nil {
		check.Error = "rule fork failed: " + err.Error()
		return check
	}
	var reply ruleForkReply
	retryUsage, replyText, err := forkReplyJSON(ctx, agent, settings, fork, "verdict", &reply, text.JSONOnlyRetry)
	check.Usage = AddUsage(fork.Usage, retryUsage)
	if err != nil {
		check.Error = err.Error()
		check.RawReply = replyText
		return check
	}
	verdict, err := ruleVerdictFromChecks(reply)
	if err != nil {
		check.Error = err.Error()
		check.RawReply = replyText
		return check
	}
	check.Verdict = verdict
	check.Checks = reply.Checks
	if reply.Findings != nil {
		check.Findings = reply.Findings
	}
	return check
}

// ruleVerdictFromChecks takes the verdict from the per-requirement checks, not from the
// model's own "verdict" field: a fork that lists a FAIL check but says PASS still fails.
// A reply with no checks did not walk the rule, so it has no verdict.
func ruleVerdictFromChecks(reply ruleForkReply) (RuleVerdict, error) {
	if len(reply.Checks) == 0 {
		return RuleVerdictError, errors.New("rule fork returned no requirement checks")
	}
	for _, requirement := range reply.Checks {
		if strings.EqualFold(requirement.Status, string(RuleVerdictFail)) {
			return RuleVerdictFail, nil
		}
	}
	return RuleVerdictPass, nil
}

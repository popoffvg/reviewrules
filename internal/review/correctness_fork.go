package review

import (
	"context"
	"fmt"

	"charm.land/fantasy"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

// maxHypotheses caps the generator's list; the generator prompt states the same cap.
const maxHypotheses = 10

// HypothesisVerdict is a checker fork's answer to one hypothesis.
type HypothesisVerdict string

// HypothesisVerdict values; HypothesisVerdictUnknown is the zero value and never a checker's answer.
const (
	HypothesisVerdictUnknown   HypothesisVerdict = ""
	HypothesisVerdictConfirmed HypothesisVerdict = "CONFIRMED"
	HypothesisVerdictRefuted   HypothesisVerdict = "REFUTED"
	HypothesisVerdictUnsure    HypothesisVerdict = "UNSURE"
)

// Hypothesis is one yes/no question about the diff; YES means a defect at At.
type Hypothesis struct {
	ID       string `json:"id"`
	Hunt     string `json:"hunt"`
	At       string `json:"at"`
	Question string `json:"question"`
	Look     string `json:"look"`
}

// HypothesisCheck is the outcome of one checker fork.
// An error leaves the verdict UNSURE: a failed check never counts as a defect.
type HypothesisCheck struct {
	Hypothesis
	Verdict  HypothesisVerdict `json:"verdict"`
	Evidence []string          `json:"evidence"`
	Scenario string            `json:"scenario,omitempty"`
	Edit     string            `json:"edit,omitempty"`
	Error    string            `json:"error,omitempty"`
	Usage    fantasy.Usage     `json:"usage"`
}

type hypothesisListReply struct {
	Hypotheses []Hypothesis `json:"hypotheses"`
}

type hypothesisCheckReply struct {
	Verdict  HypothesisVerdict `json:"verdict"`
	Evidence []string          `json:"evidence"`
	Scenario string            `json:"scenario"`
	Edit     string            `json:"edit"`
}

// GenerateHypothesesInFork forks the parent session to write at most maxHypotheses questions.
func GenerateHypothesesInFork(
	ctx context.Context,
	agent ReviewForkAgent,
	settings ForkSettings,
	text prompts.Set,
) ([]Hypothesis, fantasy.Usage, error) {
	fork, err := agent.ForkParentSession(ctx, text.HypothesisGenerator, settings)
	if err != nil {
		return nil, fantasy.Usage{}, fmt.Errorf("hypothesis generator fork failed: %w", err)
	}
	var reply hypothesisListReply
	retryUsage, _, err := forkReplyJSON(ctx, agent, settings, fork, "hypotheses", &reply, text.JSONOnlyRetry)
	usage := AddUsage(fork.Usage, retryUsage)
	if err != nil {
		return nil, usage, fmt.Errorf("hypothesis generator reply: %w", err)
	}
	hypotheses := reply.Hypotheses
	if len(hypotheses) == 0 {
		return nil, usage, fmt.Errorf("hypothesis generator reply: hypotheses list is empty")
	}
	if len(hypotheses) > maxHypotheses {
		hypotheses = hypotheses[:maxHypotheses]
	}
	for index := range hypotheses {
		if hypotheses[index].ID == "" {
			hypotheses[index].ID = fmt.Sprintf("H%d", index+1)
		}
	}
	return hypotheses, usage, nil
}

type hypothesisLedgerReply struct {
	Verdicts []hypothesisLedgerVerdict `json:"verdicts"`
}

type hypothesisLedgerVerdict struct {
	ID string `json:"id"`
	hypothesisCheckReply
}

// CheckHypothesesInFork forks the parent once with the whole ledger. The fork is not the generator,
// so the checker does not inherit the generator's reasons for suspecting the code.
// The fork usage is stored on the first check; later checks carry none, so a sum counts the call once.
func CheckHypothesesInFork(
	ctx context.Context,
	agent ReviewForkAgent,
	settings ForkSettings,
	hypotheses []Hypothesis,
	text prompts.Set,
) []HypothesisCheck {
	checks := make([]HypothesisCheck, len(hypotheses))
	for index, hypothesis := range hypotheses {
		checks[index] = HypothesisCheck{
			Hypothesis: hypothesis,
			Verdict:    HypothesisVerdictUnsure,
			Evidence:   []string{},
		}
	}
	if len(hypotheses) == 0 {
		return checks
	}
	items := make([]prompts.HypothesisCheckItem, len(hypotheses))
	for index, hypothesis := range hypotheses {
		items[index] = prompts.HypothesisCheckItem{
			ID:       hypothesis.ID,
			Hunt:     hypothesis.Hunt,
			At:       hypothesis.At,
			Question: hypothesis.Question,
			Look:     hypothesis.Look,
		}
	}
	prompt := text.HypothesisCheck(prompts.HypothesisCheckInput{Hypotheses: items})
	fork, err := agent.ForkHypothesisLedger(ctx, prompt, settings, hypotheses)
	if err != nil {
		for index := range checks {
			checks[index].Error = "hypothesis ledger fork failed: " + err.Error()
		}
		return checks
	}
	var reply hypothesisLedgerReply
	retryUsage, _, err := forkReplyJSON(ctx, agent, settings, fork, "verdicts", &reply, text.JSONOnlyRetry)
	usage := AddUsage(fork.Usage, retryUsage)
	checks[0].Usage = usage
	if err != nil {
		for index := range checks {
			checks[index].Error = err.Error()
		}
		return checks
	}
	byID := map[string]hypothesisLedgerVerdict{}
	for _, verdict := range reply.Verdicts {
		byID[verdict.ID] = verdict
	}
	for index := range checks {
		verdict, ok := byID[checks[index].ID]
		if !ok {
			checks[index].Error = "hypothesis ledger reply has no verdict for " + checks[index].ID
			continue
		}
		applyHypothesisVerdict(&checks[index], verdict.hypothesisCheckReply)
	}
	return checks
}

func applyHypothesisVerdict(check *HypothesisCheck, reply hypothesisCheckReply) {
	switch reply.Verdict {
	case HypothesisVerdictConfirmed, HypothesisVerdictRefuted, HypothesisVerdictUnsure:
		check.Verdict = reply.Verdict
	default:
		check.Error = "hypothesis fork verdict is not CONFIRMED, REFUTED, or UNSURE: " + string(reply.Verdict)
		return
	}
	if reply.Evidence != nil {
		check.Evidence = reply.Evidence
	}
	// A CONFIRMED verdict with no input scenario is a nit, not a defect.
	if check.Verdict == HypothesisVerdictConfirmed && reply.Scenario == "" {
		check.Verdict = HypothesisVerdictUnsure
		check.Error = "CONFIRMED without a scenario; downgraded to UNSURE"
	}
	if check.Verdict == HypothesisVerdictConfirmed {
		check.Scenario = reply.Scenario
		check.Edit = reply.Edit
	}
}

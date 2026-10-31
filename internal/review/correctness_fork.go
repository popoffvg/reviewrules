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
) ([]Hypothesis, fantasy.Usage, error) {
	fork, err := agent.ForkParentSession(ctx, prompts.HypothesisGenerator, settings)
	if err != nil {
		return nil, fantasy.Usage{}, fmt.Errorf("hypothesis generator fork failed: %w", err)
	}
	var reply hypothesisListReply
	retryUsage, _, err := forkReplyJSON(ctx, agent, settings, fork, "hypotheses", &reply)
	usage := AddUsage(fork.Usage, retryUsage)
	if err != nil {
		return nil, usage, fmt.Errorf("hypothesis generator reply: %w", err)
	}
	hypotheses := reply.Hypotheses
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

// CheckHypothesisInFork forks the parent session, not the generator, so the checker does not
// inherit the generator's reasons for suspecting the code.
func CheckHypothesisInFork(
	ctx context.Context,
	agent ReviewForkAgent,
	settings ForkSettings,
	hypothesis Hypothesis,
) HypothesisCheck {
	check := HypothesisCheck{
		Hypothesis: hypothesis,
		Verdict:    HypothesisVerdictUnsure,
		Evidence:   []string{},
	}
	prompt := prompts.HypothesisCheck(prompts.HypothesisCheckInput{
		ID:       hypothesis.ID,
		At:       hypothesis.At,
		Question: hypothesis.Question,
		Look:     hypothesis.Look,
	})
	fork, err := agent.ForkParentSession(ctx, prompt, settings)
	if err != nil {
		check.Error = "hypothesis fork failed: " + err.Error()
		return check
	}
	var reply hypothesisCheckReply
	retryUsage, _, err := forkReplyJSON(ctx, agent, settings, fork, "verdict", &reply)
	check.Usage = AddUsage(fork.Usage, retryUsage)
	if err != nil {
		check.Error = err.Error()
		return check
	}
	switch reply.Verdict {
	case HypothesisVerdictConfirmed, HypothesisVerdictRefuted, HypothesisVerdictUnsure:
		check.Verdict = reply.Verdict
	default:
		check.Error = "hypothesis fork verdict is not CONFIRMED, REFUTED, or UNSURE: " + string(reply.Verdict)
		return check
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
	return check
}

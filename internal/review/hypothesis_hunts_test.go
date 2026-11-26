package review

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/prompts"
)

func TestEachGeneratorForkWalksOnlyTheHuntsOfItsGroup(t *testing.T) {
	agent := &hypothesisTestAgent{reply: `{"hypotheses":[]}`}

	_, _, err := generateHypothesesByHuntGroup(t.Context(), agent, make(chan struct{}, 2), ForkSettings{}, prompts.Default(), prompts.HypothesisGeneratorInput{})

	if err != nil {
		t.Fatal(err)
	}
	if len(agent.generators) != len(hypothesisHuntGroups()) {
		t.Fatalf("generator forks = %d, want one per hunt group (%d)", len(agent.generators), len(hypothesisHuntGroups()))
	}
	for _, hunt := range hypothesisHunts {
		row := "| " + hunt.Name + " |"
		var forks int
		for _, prompt := range agent.generators {
			if strings.Contains(prompt, row) {
				forks++
			}
		}
		if forks != 1 {
			t.Errorf("hunt %s is in %d generator prompts, want 1", hunt.Name, forks)
		}
	}
}

func TestFailedHuntGroupKeepsTheQuestionsOfTheOtherGroups(t *testing.T) {
	agent := huntGroupFailureAgent{
		hypothesisTestAgent: &hypothesisTestAgent{reply: `{"hypotheses":[{"slug":"empty-id","at":"a.go:1","question":"Does an empty id pass?"}]}`},
		failingRow:          "| Concurrency |",
	}

	got, _, err := generateHypothesesByHuntGroup(t.Context(), agent, make(chan struct{}, 2), ForkSettings{}, prompts.Default(), prompts.HypothesisGeneratorInput{})

	if err == nil || !strings.Contains(err.Error(), "hunt group interleaving") {
		t.Fatalf("error = %v, want the interleaving group named", err)
	}
	if len(got) != len(hypothesisHuntGroups())-1 {
		t.Fatalf("questions = %d, want one from each group that answered", len(got))
	}
}

type huntGroupFailureAgent struct {
	*hypothesisTestAgent
	failingRow string
}

func (agent huntGroupFailureAgent) ForkParentSession(ctx context.Context, prompt string, settings ForkSettings) (ForkReply, error) {
	if strings.Contains(prompt, agent.failingRow) {
		return ForkReply{}, errors.New("backend refused the fork")
	}
	return agent.hypothesisTestAgent.ForkParentSession(ctx, prompt, settings)
}

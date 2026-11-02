package review

import (
	"context"
	"strings"
	"testing"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/prompts"
)

func TestGenerateHypothesesRejectsAnEmptyList(t *testing.T) {
	_, _, err := GenerateHypothesesInFork(
		t.Context(),
		scriptForkAgent{text: `{"hypotheses": []}`},
		ForkSettings{},
		prompts.Default(),
	)
	if err == nil || !strings.Contains(err.Error(), "hypotheses list is empty") {
		t.Fatalf("error = %v; want hypotheses list is empty", err)
	}
}

func TestCheckHypothesesReadsTheLedger(t *testing.T) {
	ledger := []Hypothesis{
		{ID: "H1", At: "a.go:1", Question: "Does the last second overflow?"},
		{ID: "H2", At: "a.go:2", Question: "Does an empty id pass?"},
	}
	reply := `{"verdicts": [` +
		`{"id": "H1", "verdict": "REFUTED", "evidence": ["a.go:1 — seconds > max — the last second is refused"]},` +
		`{"id": "H2", "verdict": "CONFIRMED", "evidence": ["a.go:2 — empty text — read as zero"], "scenario": "empty text -> zero id", "edit": "a.go:2 — reject empty text"}` +
		`]}`
	checks := CheckHypothesesInFork(t.Context(), scriptForkAgent{text: reply}, ForkSettings{}, ledger, prompts.Default())
	if len(checks) != 2 {
		t.Fatalf("checks = %d; want 2", len(checks))
	}
	if checks[0].Verdict != HypothesisVerdictRefuted || checks[1].Verdict != HypothesisVerdictConfirmed {
		t.Fatalf("verdicts = %s, %s; want REFUTED and CONFIRMED", checks[0].Verdict, checks[1].Verdict)
	}
	if checks[1].Scenario != "empty text -> zero id" {
		t.Fatalf("scenario = %q", checks[1].Scenario)
	}
}

func TestCheckHypothesesMarksAMissingVerdictUnsure(t *testing.T) {
	ledger := []Hypothesis{{ID: "H1", At: "a.go:1", Question: "q"}, {ID: "H2", At: "a.go:2", Question: "q2"}}
	checks := CheckHypothesesInFork(
		t.Context(),
		scriptForkAgent{text: `{"verdicts": [{"id": "H1", "verdict": "REFUTED", "evidence": ["a.go:1 — line"]}]}`},
		ForkSettings{},
		ledger,
		prompts.Default(),
	)
	if checks[0].Verdict != HypothesisVerdictRefuted {
		t.Fatalf("H1 = %s; want REFUTED", checks[0].Verdict)
	}
	if checks[1].Verdict != HypothesisVerdictUnsure || !strings.Contains(checks[1].Error, "no verdict for H2") {
		t.Fatalf("H2 = %+v; want UNSURE and a missing-verdict error", checks[1])
	}
}

func TestGenerateHypothesesKeepsOneHypothesis(t *testing.T) {
	got, _, err := GenerateHypothesesInFork(
		t.Context(),
		scriptForkAgent{text: `{"hypotheses": [{"id": "H1", "hunt": "Boundaries", "at": "a.go:1", "question": "Does the last second overflow?", "look": "snapid.go"}]}`},
		ForkSettings{},
		prompts.Default(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "H1" || got[0].At != "a.go:1" {
		t.Fatalf("hypotheses = %+v; want the one hypothesis", got)
	}
}

// scriptForkAgent returns one fixed fork reply and never retries.
type scriptForkAgent struct {
	text string
}

func (a scriptForkAgent) StartParentSession(context.Context, string, ForkSettings) (fantasy.Usage, error) {
	return fantasy.Usage{}, nil
}

func (a scriptForkAgent) ForkParentSession(context.Context, string, ForkSettings) (ForkReply, error) {
	return ForkReply{ForkID: "1", Text: a.text}, nil
}

func (a scriptForkAgent) ForkHypothesisLedger(ctx context.Context, prompt string, settings ForkSettings, _ []Hypothesis) (ForkReply, error) {
	return a.ForkParentSession(ctx, prompt, settings)
}

func (a scriptForkAgent) AppendToFork(context.Context, string, string, ForkSettings) (ForkReply, error) {
	return ForkReply{}, context.Canceled
}

package review

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"sync"
	"testing"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/prompts"
	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

const lightRuleConfig = `{"rules":[{"id":"no-todo","rule":"An added line has no TODO.","severity":"medium","profile":"light"}]}`

func TestLightRuleRerunChecksOnlyTheHunksThatChanged(t *testing.T) {
	repo := initHypothesisRepo(t)
	writeHypothesisFile(t, repo, ".reviewrules/config.json", lightRuleConfig)
	runHypothesisGit(t, repo, "switch", "-c", "light-rule-branch")
	writeHypothesisFile(t, repo, "fail.txt", "TODO\n")
	writeHypothesisFile(t, repo, "pass.txt", "done\n")
	cfg := ReviewRunConfig{RepoRoot: repo, Base: "review-branch", MaxDiffBytes: 100_000, HypothesisCacheDir: t.TempDir(), Parallel: 2}
	failing := map[string]bool{"fail.txt": true}
	first := runLightRuleReview(t, cfg, failing)
	again := runLightRuleReview(t, cfg, failing)
	writeHypothesisFile(t, repo, "pass.txt", "done again\n")

	afterFix := runLightRuleReview(t, cfg, failing)

	if got := first.askedFiles(); !slices.Equal(got, []string{"fail.txt", "pass.txt"}) {
		t.Errorf("first run asked about %v, want both files", got)
	}
	if got := again.askedFiles(); len(got) != 0 {
		t.Errorf("unchanged run asked about %v, want no hunk asked", got)
	}
	if got := afterFix.askedFiles(); !slices.Equal(got, []string{"pass.txt"}) {
		t.Errorf("run after a change asked about %v, want only pass.txt", got)
	}
	for _, run := range []lightRuleRun{first, again, afterFix} {
		want := []RuleFinding{{File: "fail.txt", Line: 1, Message: "TODO"}}
		if run.rule.Verdict != RuleVerdictFail || !slices.Equal(run.rule.Findings, want) {
			t.Errorf("rule = %s with %v, want FAIL with %v", run.rule.Verdict, run.rule.Findings, want)
		}
	}
}

func TestLightRuleAsksAgainOnlyForTheHunkAReplySkipped(t *testing.T) {
	repo := initHypothesisRepo(t)
	writeHypothesisFile(t, repo, ".reviewrules/config.json", lightRuleConfig)
	runHypothesisGit(t, repo, "switch", "-c", "light-rule-branch")
	writeHypothesisFile(t, repo, "answered.txt", "done\n")
	writeHypothesisFile(t, repo, "skipped.txt", "done\n")
	cfg := ReviewRunConfig{RepoRoot: repo, Base: "review-branch", MaxDiffBytes: 100_000, HypothesisCacheDir: t.TempDir(), Parallel: 2}
	skipping := lightRuleTestAgent{skip: "skipped.txt"}
	partial := runLightRuleReviewWith(t, cfg, &skipping)

	next := runLightRuleReview(t, cfg, nil)

	if partial.rule.Verdict != RuleVerdictError || partial.rule.Error == "" {
		t.Errorf("rule with a skipped hunk = %s (%q), want ERROR with the skipped hunk named", partial.rule.Verdict, partial.rule.Error)
	}
	if got := next.askedFiles(); !slices.Equal(got, []string{"skipped.txt"}) || next.rule.Verdict != RuleVerdictPass {
		t.Errorf("next run asked about %v with verdict %s, want only skipped.txt and PASS", got, next.rule.Verdict)
	}
}

func TestLightRuleTakesNoFindingOutsideTheLinesItsHunkAdds(t *testing.T) {
	repo := initHypothesisRepo(t)
	writeHypothesisFile(t, repo, ".reviewrules/config.json", lightRuleConfig)
	runHypothesisGit(t, repo, "switch", "-c", "light-rule-branch")
	writeHypothesisFile(t, repo, "fail.txt", "TODO\n")
	cfg := ReviewRunConfig{RepoRoot: repo, Base: "review-branch", MaxDiffBytes: 100_000, HypothesisCacheDir: t.TempDir(), Parallel: 2}
	misplaced := lightRuleTestAgent{failing: map[string]bool{"fail.txt": true}, findingLine: 99}
	first := runLightRuleReviewWith(t, cfg, &misplaced)

	next := runLightRuleReview(t, cfg, map[string]bool{"fail.txt": true})

	if first.rule.Verdict != RuleVerdictError || len(first.rule.Findings) != 0 {
		t.Errorf("rule with a finding at fail.txt:99 = %s with %v, want ERROR and no finding", first.rule.Verdict, first.rule.Findings)
	}
	if got := next.askedFiles(); !slices.Equal(got, []string{"fail.txt"}) || next.rule.Verdict != RuleVerdictFail {
		t.Errorf("next run asked about %v with verdict %s, want fail.txt asked again and FAIL", got, next.rule.Verdict)
	}
}

func TestLightRuleStoresNoVerdictWhileATrackedFileHasAnUncommittedEdit(t *testing.T) {
	repo := initHypothesisRepo(t)
	writeHypothesisFile(t, repo, ".reviewrules/config.json", lightRuleConfig)
	runHypothesisGit(t, repo, "switch", "-c", "light-rule-branch")
	writeHypothesisFile(t, repo, "pass.txt", "done\n")
	cfg := ReviewRunConfig{RepoRoot: repo, Base: "review-branch", MaxDiffBytes: 100_000, HypothesisCacheDir: t.TempDir(), Parallel: 2}
	editWithoutCommit(t, repo, "base.txt")
	runLightRuleReview(t, cfg, nil)
	runHypothesisGit(t, repo, "checkout", "--", "base.txt")

	clean := runLightRuleReview(t, cfg, nil)

	if got := clean.askedFiles(); !slices.Equal(got, []string{"pass.txt"}) {
		t.Errorf("run on a clean tree asked about %v, want pass.txt asked again", got)
	}
}

func TestVerdictKeysKeepTwoHunksWithTheSameLinesApart(t *testing.T) {
	hunk := DiffHunk{File: "same.go", Header: "@@ -1 +1 @@", NewStart: 1, Body: "+x\n"}
	moved := hunk
	moved.Header, moved.NewStart = "@@ -9 +9 @@", 9
	review := ruleHunkReview{text: prompts.Default(), hunks: []DiffHunk{hunk, moved}}

	keys := review.verdictKeys(rulecatalog.ReviewRule{ID: "no-todo", Text: "An added line has no TODO."}, ForkSettings{})

	if keys[0] == keys[1] {
		t.Errorf("two hunks with the same lines in one file got one key %s", keys[0])
	}
}

type lightRuleRun struct {
	rule  RuleCheckResult
	agent *lightRuleTestAgent
}

func (run lightRuleRun) askedFiles() []string {
	return run.agent.asked
}

func runLightRuleReview(t *testing.T, cfg ReviewRunConfig, failing map[string]bool) lightRuleRun {
	t.Helper()
	return runLightRuleReviewWith(t, cfg, &lightRuleTestAgent{failing: failing})
}

func runLightRuleReviewWith(t *testing.T, cfg ReviewRunConfig, agent *lightRuleTestAgent) lightRuleRun {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	report, err := RunReview(t.Context(), logger, func(string) ReviewForkAgent { return agent }, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Rules) != 1 {
		t.Fatalf("report has %d rules, want the one light rule", len(report.Rules))
	}
	slices.Sort(agent.asked)
	return lightRuleRun{rule: report.Rules[0], agent: agent}
}

var listedHunkPattern = regexp.MustCompile(`(?m)^- (h\d+): (\S+) @@`)

// lightRuleTestAgent answers each listed hunk FAIL with one finding at findingLine (line 1 when zero) when its file
// is in failing, else PASS, and leaves out the hunk of the file skip.
type lightRuleTestAgent struct {
	lock        sync.Mutex
	failing     map[string]bool
	findingLine int
	skip        string
	asked       []string
}

func (*lightRuleTestAgent) StartParentSession(context.Context, string, ForkSettings) (fantasy.Usage, error) {
	return fantasy.Usage{}, nil
}

func (agent *lightRuleTestAgent) ForkParentSession(_ context.Context, prompt string, _ ForkSettings) (ForkReply, error) {
	agent.lock.Lock()
	defer agent.lock.Unlock()
	reply := ruleHunkReply{}
	for _, listed := range listedHunkPattern.FindAllStringSubmatch(prompt, -1) {
		id, file := listed[1], listed[2]
		agent.asked = append(agent.asked, file)
		if file == agent.skip {
			continue
		}
		answer := ruleHunkAnswer{Hunk: id, Status: "PASS", Evidence: "no added line of this kind"}
		if agent.failing[file] {
			answer.Status = "FAIL"
			answer.Findings = []RuleFinding{{File: file, Line: max(agent.findingLine, 1), Message: "TODO"}}
		}
		reply.Hunks = append(reply.Hunks, answer)
	}
	data, err := json.Marshal(reply)
	return ForkReply{Text: hypothesisJSON(string(data))}, err
}

func (*lightRuleTestAgent) ForkHypothesisLedger(context.Context, string, ForkSettings, []Hypothesis) (ForkReply, error) {
	return ForkReply{}, fmt.Errorf("unexpected hypothesis ledger")
}

func (*lightRuleTestAgent) AppendToFork(context.Context, string, string, ForkSettings) (ForkReply, error) {
	return ForkReply{}, fmt.Errorf("unexpected rule retry")
}

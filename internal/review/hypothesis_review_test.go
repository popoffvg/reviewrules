package review

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/fantasy"
)

func TestReviewHypothesesAcrossBranchRuns(t *testing.T) {
	repo := t.TempDir()
	runHypothesisGit(t, repo, "init", "-b", "main")
	runHypothesisGit(t, repo, "config", "user.name", "Review Test")
	runHypothesisGit(t, repo, "config", "user.email", "review@example.invalid")
	runHypothesisGit(t, repo, "config", "commit.gpgsign", "false")
	err := os.Mkdir(filepath.Join(repo, ".reviewrules"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	writeHypothesisFile(t, repo, ".reviewrules/config.json", "{}\n")
	writeHypothesisFile(t, repo, "base.txt", "base\n")
	firstBranch := "review-branch"
	runHypothesisGit(t, repo, "switch", "-c", firstBranch)
	writeHypothesisFile(t, repo, "first.txt", "first-branch-change\n")
	cfg := ReviewRunConfig{
		RepoRoot: repo, Base: "main", MaxDiffBytes: 100_000, CorrectnessEnabled: true,
		HypothesisCacheDir: t.TempDir(), Parallel: 2,
	}
	first := runHypothesisReview(t, cfg, `{"hypotheses":[{"slug":"first-defect","at":"first.txt:1","question":"Can the first change fail?"}]}`)
	firstCommit := first.report.Commit
	if firstCommit == "" || len(first.agent.generators) != 1 || len(first.report.Correctness.HypothesisCheck) != 1 {
		t.Fatalf("first review = %+v", first.report)
	}
	if got := first.report.Correctness.HypothesisCheck[0].Commit; got != firstCommit {
		t.Errorf("first hypothesis commit = %s, want %s", got, firstCommit)
	}
	again := runHypothesisReview(t, cfg, "")
	if len(again.agent.generators) != 0 || len(again.agent.checks) != 1 || again.report.Correctness.GeneratorUsage.InputTokens != 0 {
		t.Fatal("unchanged review must skip generation and run the old check")
	}
	runHypothesisGit(t, repo, "commit", "--allow-empty", "-m", "Advance branch HEAD")
	newHead := runHypothesisReview(t, cfg, `{"hypotheses":[]}`)
	if newHead.report.Commit == firstCommit || len(newHead.agent.generators) != 1 || len(newHead.agent.checks) != 1 {
		t.Fatal("new HEAD with an unchanged patch must run both generation and the old check")
	}
	cfg.HypothesisModelName = "other-generator"
	newModel := runHypothesisReview(t, cfg, `{"hypotheses":[]}`)
	if len(newModel.agent.generators) != 1 || len(newModel.agent.checks) != 1 {
		t.Fatal("changed generator model must run generation and the old check")
	}
	err = os.WriteFile(filepath.Join(repo, ".reviewrules", "config.json"), []byte(`{"instructions":"Check empty values."}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	newContext := runHypothesisReview(t, cfg, `{"hypotheses":[]}`)
	if len(newContext.agent.generators) != 1 || len(newContext.agent.checks) != 1 {
		t.Fatal("changed instructions must regenerate and run the old check")
	}

	writeHypothesisFile(t, repo, "second.txt", "second-branch-change\n")
	next := runHypothesisReview(t, cfg, `{"hypotheses":[{"slug":"first-defect","at":"first.txt:1","question":"Can the first change fail?","hunt":"Callers","look":"updated caller"},{"slug":"first-defect","at":"second.txt:1","question":"Can the first change fail?"}]}`)
	if len(next.agent.generators) != 1 || len(next.agent.checks) != 1 || len(next.report.Correctness.HypothesisCheck) != 2 {
		t.Fatalf("next review launched %d generators and %d checks", len(next.agent.generators), len(next.agent.checks))
	}
	prompt := next.agent.generators[0]
	for _, fragment := range []string{"+second-branch-change", "first-defect", "Can the first change fail?"} {
		if !strings.Contains(prompt, fragment) {
			t.Errorf("delta prompt lacks %q", fragment)
		}
	}
	if strings.Contains(prompt, "+first-branch-change") {
		t.Error("delta prompt contains the earlier commit's patch")
	}
	checks := next.report.Correctness.HypothesisCheck
	if checks[0].Commit != firstCommit || checks[1].Commit != next.report.Commit || checks[0].ID == checks[1].ID || checks[0].Slug == checks[1].Slug {
		t.Errorf("hypothesis identities do not match their source commits: %+v", checks)
	}
	if checks[0].Hunt != "Callers" || checks[0].Look != "updated caller" {
		t.Error("repeated hypothesis did not retain the new caller context")
	}

	runHypothesisGit(t, repo, "switch", "-c", "another-branch")
	coldReply := `{"hypotheses":[{"slug":"cold-defect","question":"Can the cold change fail?"}]}`
	other := runHypothesisReview(t, cfg, coldReply)
	if len(other.agent.generators) != 1 || len(other.report.Correctness.HypothesisCheck) != 1 {
		t.Fatal("another branch reused the first branch's hypotheses")
	}
	runHypothesisGit(t, repo, "switch", firstBranch)
	cfg.IncludeWorktree = true
	err = os.WriteFile(filepath.Join(repo, "second.txt"), []byte("dirty-change\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	dirty := runHypothesisReview(t, cfg, `{"hypotheses":[]}`)
	if len(dirty.agent.generators) != 1 || strings.Contains(dirty.agent.generators[0], "affected by the delta below") || len(dirty.report.Correctness.HypothesisCheck) != 2 {
		t.Fatal("changed worktree must use the full captured diff")
	}
	dirtyAgain := runHypothesisReview(t, cfg, "")
	if len(dirtyAgain.agent.generators) != 0 || len(dirtyAgain.report.Correctness.HypothesisCheck) != 2 {
		t.Fatal("unchanged worktree did not reuse the questions and run the checks")
	}
	err = os.WriteFile(filepath.Join(repo, "second.txt"), []byte("later-dirty-change\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	dirtyChanged := runHypothesisReview(t, cfg, `{"hypotheses":[]}`)
	if len(dirtyChanged.agent.generators) != 1 || len(dirtyChanged.report.Correctness.HypothesisCheck) != 2 {
		t.Fatal("changed worktree did not regenerate and verify earlier questions")
	}
	if strings.Contains(dirtyChanged.agent.generators[0], "affected by the delta below") {
		t.Error("changed worktree used an incomplete delta for generation")
	}

	runHypothesisGit(t, repo, "switch", "--detach")
	cfg.IncludeWorktree = false
	detached := runHypothesisReview(t, cfg, coldReply)
	if len(detached.agent.generators) != 1 || len(detached.report.Correctness.HypothesisCheck) != 1 {
		t.Fatal("detached review used the branch's cached questions")
	}

	runHypothesisGit(t, repo, "switch", firstBranch)
	writeHypothesisFile(t, repo, "third.txt", "third-branch-change\n")
	newQuestions := make([]Hypothesis, maxHypotheses)
	for i := range newQuestions {
		newQuestions[i].Question = fmt.Sprintf("Can input %d cause the third change to fail?", i)
	}
	reply, err := json.Marshal(hypothesisListReply{Hypotheses: newQuestions})
	if err != nil {
		t.Fatal(err)
	}
	batched := runHypothesisReview(t, cfg, string(reply))
	if len(batched.agent.checks) != 2 || len(batched.report.Correctness.HypothesisCheck) != maxHypotheses+2 {
		t.Fatal("checker must verify the full ledger in batches")
	}

	index, err := openWorktreeIndex(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if index.file == nil {
		t.Fatal("expected an index for the test repository")
	}
	t.Cleanup(func() { _ = index.file.Close() })
	runHypothesisGit(t, repo, "switch", "--detach", "HEAD~1")
	runHypothesisGit(t, repo, "switch", firstBranch)
	if err = index.checkUnchanged(); err == nil {
		t.Fatal("worktree index comparison did not detect the two checkouts")
	}
}

type hypothesisRun struct {
	report ReviewReport
	agent  *hypothesisTestAgent
}

func TestMergeHypothesesKeepsCaseSensitiveQuestions(t *testing.T) {
	previous := []Hypothesis{{Slug: "case-sensitive", At: "status.go:1", Question: `Does "READY" pass?`}}
	generated := []Hypothesis{{Slug: "case-sensitive", At: "status.go:1", Question: `Does "ready" pass?`}}
	if got := mergeHypotheses(previous, generated, "commit"); len(got) != 2 {
		t.Fatalf("merged %d questions, want two case-sensitive questions", len(got))
	}
}

func runHypothesisReview(t *testing.T, cfg ReviewRunConfig, generatorReply string) hypothesisRun {
	t.Helper()
	agent := &hypothesisTestAgent{reply: generatorReply}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	report, err := RunReview(t.Context(), logger, func(string) ReviewForkAgent { return agent }, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Correctness.GeneratorError != "" {
		t.Fatal(report.Correctness.GeneratorError)
	}
	return hypothesisRun{report: report, agent: agent}
}

func runHypothesisGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	_, err := runGit(t.Context(), repo, args...)
	if err != nil {
		t.Fatal(err)
	}
}

func writeHypothesisFile(t *testing.T, repo string, file string, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(repo, file), []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	runHypothesisGit(t, repo, "add", "--", file)
	runHypothesisGit(t, repo, "commit", "-m", "Add "+file)
}

type hypothesisTestAgent struct {
	lock       sync.Mutex
	reply      string
	generators []string
	checks     []string
}

func (*hypothesisTestAgent) StartParentSession(context.Context, string, ForkSettings) (fantasy.Usage, error) {
	return fantasy.Usage{}, nil
}

func (agent *hypothesisTestAgent) ForkParentSession(_ context.Context, prompt string, _ ForkSettings) (ForkReply, error) {
	agent.lock.Lock()
	defer agent.lock.Unlock()
	if strings.HasPrefix(prompt, "# New task: write correctness hypotheses") {
		agent.generators = append(agent.generators, prompt)
		if agent.reply != "" {
			return ForkReply{Text: hypothesisJSON(agent.reply), Usage: fantasy.Usage{InputTokens: 1}}, nil
		}
	}
	return ForkReply{}, fmt.Errorf("unexpected hypothesis fork: %s", prompt)
}

func (agent *hypothesisTestAgent) ForkHypothesisLedger(_ context.Context, prompt string, _ ForkSettings, hypotheses []Hypothesis) (ForkReply, error) {
	agent.lock.Lock()
	defer agent.lock.Unlock()
	agent.checks = append(agent.checks, prompt)
	reply := hypothesisLedgerReply{}
	for _, hypothesis := range hypotheses {
		reply.Verdicts = append(reply.Verdicts, hypothesisLedgerVerdict{
			ID: hypothesis.ID, hypothesisCheckReply: hypothesisCheckReply{Verdict: HypothesisVerdictRefuted},
		})
	}
	data, err := json.Marshal(reply)
	return ForkReply{Text: hypothesisJSON(string(data))}, err
}

func (*hypothesisTestAgent) AppendToFork(context.Context, string, string, ForkSettings) (ForkReply, error) {
	return ForkReply{}, fmt.Errorf("unexpected hypothesis retry")
}

func hypothesisJSON(reply string) string {
	return "```json\n" + reply + "\n```"
}

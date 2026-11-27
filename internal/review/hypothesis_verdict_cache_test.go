package review

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestReviewReusesVerdictsWhileTheirInputsHold(t *testing.T) {
	verdicts := map[string]hypothesisCheckReply{
		"refuted":   {Verdict: HypothesisVerdictRefuted, Evidence: []string{"b.txt:1 guards the input"}},
		"confirmed": {Verdict: HypothesisVerdictConfirmed, Evidence: []string{"c.txt:1 drops the error"}, Scenario: "empty input"},
		"unsure":    {Verdict: HypothesisVerdictUnsure, Evidence: []string{"the caller is outside the repo"}},
	}
	everyQuestion := []string{"confirmed", "no-at", "refuted", "unsure"}
	refutedCiting := func(evidence string) map[string]hypothesisCheckReply {
		return map[string]hypothesisCheckReply{"refuted": {Verdict: HypothesisVerdictRefuted, Evidence: []string{evidence}}}
	}
	changeGuard := func(t *testing.T, repo string, _ *ReviewRunConfig) {
		writeNestedHypothesisFile(t, repo, "src/guard.go", "guard changed\n")
	}
	tests := []struct {
		name          string
		firstNotes    []string
		firstVerdicts map[string]hypothesisCheckReply
		prepare       func(t *testing.T, repo string, cfg *ReviewRunConfig)
		change        func(t *testing.T, repo string, cfg *ReviewRunConfig)
		wantChecked   []string
	}{
		{
			name:        "unchanged files and notes reuse every verdict of a question with an At",
			change:      func(*testing.T, string, *ReviewRunConfig) {},
			wantChecked: []string{"no-at"},
		},
		{
			name: "a changed At file checks its question again",
			change: func(t *testing.T, repo string, _ *ReviewRunConfig) {
				writeHypothesisFile(t, repo, "a.txt", "a changed\n")
			},
			wantChecked: []string{"no-at", "refuted"},
		},
		{
			name: "a changed evidence file checks its question again",
			change: func(t *testing.T, repo string, _ *ReviewRunConfig) {
				writeHypothesisFile(t, repo, "b.txt", "b changed\n")
			},
			wantChecked: []string{"no-at", "refuted"},
		},
		{
			name:          "a changed evidence file with a plus in its path checks its question again",
			firstVerdicts: map[string]hypothesisCheckReply{"refuted": {Verdict: HypothesisVerdictRefuted, Evidence: []string{"src/c++/guard.cc:12 guards the input"}}},
			change: func(t *testing.T, repo string, _ *ReviewRunConfig) {
				writeNestedHypothesisFile(t, repo, "src/c++/guard.cc", "guard changed\n")
			},
			wantChecked: []string{"no-at", "refuted"},
		},
		{
			name:          "a changed evidence file with a space in its path checks its question again",
			firstVerdicts: map[string]hypothesisCheckReply{"refuted": {Verdict: HypothesisVerdictRefuted, Evidence: []string{"src/my guard.go:12 guards the input"}}},
			change: func(t *testing.T, repo string, _ *ReviewRunConfig) {
				writeNestedHypothesisFile(t, repo, "src/my guard.go", "guard changed\n")
			},
			wantChecked: []string{"no-at", "refuted"},
		},
		{
			name:          "a changed evidence file cited with ./ checks its question again",
			firstVerdicts: refutedCiting("src/./guard.go:12 guards the input"),
			change:        changeGuard,
			wantChecked:   []string{"no-at", "refuted"},
		},
		{
			name:          "a changed evidence file cited with ../ checks its question again",
			firstVerdicts: refutedCiting("src/helpers/../guard.go:12 guards the input"),
			change:        changeGuard,
			wantChecked:   []string{"no-at", "refuted"},
		},
		{
			name:          "a changed evidence file cited by an absolute path checks its question again",
			firstVerdicts: refutedCiting("/home/dev/project/src/guard.go:12 guards the input"),
			change:        changeGuard,
			wantChecked:   []string{"no-at", "refuted"},
		},
		{
			name:          "a changed evidence file cited with backslashes checks its question again",
			firstVerdicts: refutedCiting(`src\guard.go:12 guards the input`),
			change:        changeGuard,
			wantChecked:   []string{"no-at", "refuted"},
		},
		{
			name:        "--worktree: every question is checked",
			change:      func(_ *testing.T, _ string, cfg *ReviewRunConfig) { cfg.IncludeWorktree = true },
			wantChecked: everyQuestion,
		},
		{
			name:        "--worktree: no verdict is stored",
			prepare:     func(_ *testing.T, _ string, cfg *ReviewRunConfig) { cfg.IncludeWorktree = true },
			change:      func(_ *testing.T, _ string, cfg *ReviewRunConfig) { cfg.IncludeWorktree = false },
			wantChecked: everyQuestion,
		},
		{
			name:        "an uncommitted edit: every question is checked",
			change:      func(t *testing.T, repo string, _ *ReviewRunConfig) { editWithoutCommit(t, repo, "d.txt") },
			wantChecked: everyQuestion,
		},
		{
			name:    "an uncommitted edit: no verdict is stored",
			prepare: func(t *testing.T, repo string, _ *ReviewRunConfig) { editWithoutCommit(t, repo, "d.txt") },
			change: func(t *testing.T, repo string, _ *ReviewRunConfig) {
				runHypothesisGit(t, repo, "checkout", "--", "d.txt")
			},
			wantChecked: everyQuestion,
		},
		{
			name: "an added note reuses only the REFUTED verdict",
			change: func(_ *testing.T, _ string, cfg *ReviewRunConfig) {
				cfg.RunContext = []string{"c.txt:1: the error is by design"}
			},
			wantChecked: []string{"confirmed", "no-at", "unsure"},
		},
		{
			name:        "a removed note checks every question again",
			firstNotes:  []string{"a.txt:1: the input is never empty"},
			change:      func(_ *testing.T, _ string, cfg *ReviewRunConfig) { cfg.RunContext = nil },
			wantChecked: everyQuestion,
		},
		{
			name:        "changed check settings check every question again",
			change:      func(_ *testing.T, _ string, cfg *ReviewRunConfig) { cfg.HypothesisCheckModelName = "other-checker" },
			wantChecked: everyQuestion,
		},
		{
			name:          "a check with an error is not cached",
			firstVerdicts: map[string]hypothesisCheckReply{"refuted": {Verdict: "MAYBE"}},
			change:        func(*testing.T, string, *ReviewRunConfig) {},
			wantChecked:   []string{"no-at", "refuted"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := initHypothesisRepo(t)
			for _, file := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
				writeHypothesisFile(t, repo, file, file+"\n")
			}
			cfg := ReviewRunConfig{
				RepoRoot: repo, Base: "main", MaxDiffBytes: 100_000, CorrectnessEnabled: true,
				HypothesisCacheDir: t.TempDir(), Parallel: 2, RunContext: test.firstNotes,
			}
			if test.prepare != nil {
				test.prepare(t, repo, &cfg)
			}
			firstVerdicts := maps.Clone(verdicts)
			maps.Copy(firstVerdicts, test.firstVerdicts)
			first := runVerdictReview(t, cfg, firstVerdicts)
			test.change(t, repo, &cfg)

			second := runVerdictReview(t, cfg, verdicts)

			checked := slices.Sorted(maps.Keys(second.agent.checkedSlugs()))
			if !slices.Equal(checked, test.wantChecked) {
				t.Errorf("checked %v, want %v", checked, test.wantChecked)
			}
			for _, check := range second.report.Correctness.HypothesisCheck {
				wantReusedFrom := first.report.Commit
				if slices.Contains(test.wantChecked, check.Slug) {
					wantReusedFrom = ""
				}
				if check.ReusedFrom != wantReusedFrom || (wantReusedFrom != "" && check.Usage.InputTokens != 0) {
					t.Errorf("%s reused from %q with usage %s, want reused from %q with no usage", check.Slug, check.ReusedFrom, check.Usage.String(), wantReusedFrom)
				}
			}
		})
	}
}

const verdictQuestions = `{"hypotheses":[` +
	`{"slug":"refuted","at":"a.txt:1","question":"Can empty input pass?"},` +
	`{"slug":"confirmed","at":"c.txt:1","question":"Is the error dropped?"},` +
	`{"slug":"unsure","at":"d.txt:1","question":"Does the caller retry?"},` +
	`{"slug":"no-at","question":"Does any caller skip the check?"}]}`

func editWithoutCommit(t *testing.T, repo string, file string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(repo, file), []byte("uncommitted edit\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func runVerdictReview(t *testing.T, cfg ReviewRunConfig, verdicts map[string]hypothesisCheckReply) hypothesisRun {
	t.Helper()
	return runReviewWithAgent(t, cfg, &hypothesisTestAgent{reply: verdictQuestions, verdicts: verdicts})
}

func writeNestedHypothesisFile(t *testing.T, repo string, file string, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(filepath.Join(repo, file)), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	writeHypothesisFile(t, repo, file, content)
}

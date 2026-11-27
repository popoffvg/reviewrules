package review

import (
	"context"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/popoffvg/reviewrules/internal/prompts"
)

// cachedVerdict is the last verdict of one hypothesis and what the run that judged it ran under.
type cachedVerdict struct {
	hypothesisCheckReply
	// JudgedAt is the HEAD commit of the run that judged the verdict.
	JudgedAt string `json:"judged_at"`
	// Notes are the --context notes of that run.
	Notes []string `json:"notes"`
	// CheckDigest keys the verdict to the review context, the check prompt, the backend, and the check fork settings.
	CheckDigest string `json:"check_digest"`
}

// verdictReuse decides which cached verdicts still answer their question in this run, and which verdicts the cache keeps.
type verdictReuse struct {
	cached      map[string]cachedVerdict
	checkDigest string
	head        string
	notes       []string
	// readsWorktree is set under --worktree, or when a tracked file has an uncommitted edit: the checkers read
	// files that no commit holds, so the run reuses no verdict and keeps the cached ones as they are.
	readsWorktree bool
	// changedSince maps a judged commit to the files that differ from it at head; a commit git cannot diff is absent,
	// and under --worktree every commit is absent.
	changedSince map[string][]string
}

func (review hypothesisReview) verdictReuse(
	ctx context.Context, settings ForkSettings, text prompts.Set, cached map[string]cachedVerdict,
) verdictReuse {
	reuse := verdictReuse{
		cached:        cached,
		checkDigest:   review.settingsDigest(text.HypothesisCheck(prompts.HypothesisCheckInput{}), settings),
		head:          review.diff.Head,
		notes:         review.config.RunContext,
		readsWorktree: review.config.IncludeWorktree || hasUncommittedEdits(ctx, review.config.RepoRoot),
	}
	if reuse.readsWorktree {
		return reuse
	}
	reuse.changedSince = review.changedFilesByJudgedCommit(ctx, reuse.judgedCommits())
	return reuse
}

// hasUncommittedEdits reports whether a tracked file differs from HEAD. A failed git call counts as an edit,
// so a verdict is never stored under a commit it may not describe.
func hasUncommittedEdits(ctx context.Context, repoRoot string) bool {
	status, err := runGit(ctx, repoRoot, "--no-optional-locks", "status", "--porcelain", "--untracked-files=no")
	return err != nil || strings.TrimSpace(status) != ""
}

// judgedCommits lists the judged commit of each cached verdict that the check digest and the notes still allow,
// so git diffs only the commits that can give a reuse.
func (reuse verdictReuse) judgedCommits() []string {
	var commits []string
	for _, verdict := range reuse.cached {
		if reuse.allows(verdict) && !slices.Contains(commits, verdict.JudgedAt) {
			commits = append(commits, verdict.JudgedAt)
		}
	}
	return commits
}

// judgedCommitDiffs caps the git diff processes that run at once.
const judgedCommitDiffs = 4

func (review hypothesisReview) changedFilesByJudgedCommit(ctx context.Context, commits []string) map[string][]string {
	changedSince := make(map[string][]string, len(commits))
	var lock sync.Mutex
	limit := make(chan struct{}, judgedCommitDiffs)
	var wait sync.WaitGroup
	for _, commit := range commits {
		wait.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			changed, err := changedFiles(ctx, review.config.RepoRoot, commit, review.diff.Head)
			// A commit git cannot diff, such as one a rebase dropped, stays absent, so its verdicts are checked again.
			if err != nil {
				return
			}
			lock.Lock()
			defer lock.Unlock()
			changedSince[commit] = changed
		})
	}
	wait.Wait()
	return changedSince
}

// splitByReuse gives each hypothesis whose cached verdict still holds its reused check, and returns the other
// hypotheses with their indexes in hypotheses.
func (reuse verdictReuse) splitByReuse(hypotheses []Hypothesis) ([]HypothesisCheck, []Hypothesis, []int) {
	checks := make([]HypothesisCheck, len(hypotheses))
	var open []Hypothesis
	var openIndexes []int
	for index, hypothesis := range hypotheses {
		check, reused := reuse.reusedCheck(hypothesis)
		if reused {
			checks[index] = check
			continue
		}
		open = append(open, hypothesis)
		openIndexes = append(openIndexes, index)
	}
	return checks, open, openIndexes
}

// reusedCheck returns the cached verdict of hypothesis when it still holds: the check digest and the notes allow it,
// and no file that its At or its evidence names changed since it was judged.
func (reuse verdictReuse) reusedCheck(hypothesis Hypothesis) (HypothesisCheck, bool) {
	verdict, found := reuse.cached[hypothesisQuestionKey(hypothesis)]
	if !found || !reuse.allows(verdict) || strings.TrimSpace(hypothesis.At) == "" {
		return HypothesisCheck{}, false
	}
	changed, diffed := reuse.changedSince[verdict.JudgedAt]
	if !diffed || citesChangedFile(hypothesis.At, verdict.Evidence, changed) {
		return HypothesisCheck{}, false
	}
	return HypothesisCheck{
		Hypothesis:  hypothesis,
		Verdict:     verdict.Verdict,
		BugPriority: verdict.BugPriority,
		Evidence:    verdict.Evidence,
		Scenario:    verdict.Scenario,
		Edit:        verdict.Edit,
		ReusedFrom:  verdict.JudgedAt,
	}, true
}

// allows reports whether the check digest and this run's --context notes let a verdict stand. Every note it was
// judged under must still be given. A note added since can answer a CONFIRMED or UNSURE finding, so only a
// REFUTED verdict outlives it.
func (reuse verdictReuse) allows(verdict cachedVerdict) bool {
	if verdict.CheckDigest != reuse.checkDigest || !isGitCommit(verdict.JudgedAt) {
		return false
	}
	return notesKeepVerdict(reuse.notes, verdict.Notes, verdict.Verdict == HypothesisVerdictRefuted)
}

// notesKeepVerdict reports whether a verdict judged under the judged notes stands under the given notes. Every
// judged note must still be given. A note added since can answer a finding, so only a cleared verdict outlives it.
func notesKeepVerdict(given []string, judged []string, cleared bool) bool {
	return keepsNotes(given, judged) && (cleared || keepsNotes(judged, given))
}

// citesChangedFile reports whether At or an evidence line holds the base name of a changed file followed by `:`.
// The base name matches every spelling of the path: `./`, `../`, absolute, or backslashes. An At with no line still
// names its file. A text that only looks like a citation costs one extra check, never a stale reuse.
func citesChangedFile(at string, evidence []string, changed []string) bool {
	texts := append([]string{strings.TrimSpace(at) + ":"}, evidence...)
	for _, file := range changed {
		citation := path.Base(file) + ":"
		if slices.ContainsFunc(texts, func(text string) bool { return strings.Contains(text, citation) }) {
			return true
		}
	}
	return false
}

// verdictsToCache keeps each reused verdict as it was and records each verdict this run judged; a check with an
// error has no verdict and is not kept. A --worktree run keeps the cached verdicts as they are.
func (reuse verdictReuse) verdictsToCache(checks []HypothesisCheck) map[string]cachedVerdict {
	if reuse.readsWorktree {
		return reuse.cached
	}
	verdicts := make(map[string]cachedVerdict, len(checks))
	for _, check := range checks {
		key := hypothesisQuestionKey(check.Hypothesis)
		switch {
		case check.ReusedFrom != "":
			verdicts[key] = reuse.cached[key]
		case check.Error == "":
			verdicts[key] = cachedVerdict{
				hypothesisCheckReply: hypothesisCheckReply{
					Verdict: check.Verdict, BugPriority: check.BugPriority, Evidence: check.Evidence,
					Scenario: check.Scenario, Edit: check.Edit,
				},
				JudgedAt:    reuse.head,
				Notes:       reuse.notes,
				CheckDigest: reuse.checkDigest,
			}
		}
	}
	return verdicts
}

// changedFiles lists the files that differ between two commits. A rename lists both paths, so evidence that
// cites the old path sees the change.
func changedFiles(ctx context.Context, repoRoot string, from string, to string) ([]string, error) {
	if from == to {
		return nil, nil
	}
	output, err := runGit(ctx, repoRoot, "--no-optional-locks", "diff", "--name-only", "-z", "--no-renames", from, to, "--")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, file := range strings.Split(output, "\x00") {
		if file != "" {
			files = append(files, file)
		}
	}
	return files, nil
}

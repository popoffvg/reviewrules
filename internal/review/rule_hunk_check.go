package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/digest"
	"github.com/popoffvg/reviewrules/internal/prompts"
	"github.com/popoffvg/reviewrules/internal/rulecatalog"
)

// hunksPerFork caps the hunks that one light rule fork walks, so a model with no reasoning budget answers each one.
const hunksPerFork = 20

const ruleHunkCacheSuffix = ".rulehunks.json"

// ruleHunkReview checks each light rule hunk by hunk. It reuses the verdict of a hunk whose text, rule,
// prompt, review context, and fork settings did not change, so a rerun asks only about the hunks a fix touched.
type ruleHunkReview struct {
	run    hypothesisReview
	logger *slog.Logger
	text   prompts.Set
	hunks  []DiffHunk
	cached map[string]cachedHunkVerdict
	cache  branchCache
	stamp  string
	// readsUncommittedFiles is set under --worktree or while a tracked file has an uncommitted edit: a fork can read
	// files that no commit holds, so the run stores no verdict.
	readsUncommittedFiles bool
}

// loadRuleHunkReview reads the branch's light rule verdicts. A cache that cannot be read costs a full check, not the run.
func loadRuleHunkReview(ctx context.Context, logger *slog.Logger, run hypothesisReview, text prompts.Set) ruleHunkReview {
	review := ruleHunkReview{run: run, logger: logger, text: text, hunks: diffHunks(run.diff.Patch)}
	review.readsUncommittedFiles = review.canReadUncommittedFiles(ctx)
	cache, err := branchCacheEntry(ctx, run.config, run.diff, run.ignorePatterns, ruleHunkCacheSuffix)
	if err != nil {
		logger.Warn("rule hunk cache scope unavailable", "error", err)
	}
	var snapshot ruleHunkSnapshot
	_, stamp, err := cache.loadJSON(ctx, &snapshot)
	if err != nil {
		logger.Warn("rule hunk cache read failed", "error", err)
		snapshot = ruleHunkSnapshot{}
	}
	review.cache, review.stamp, review.cached = cache, stamp, snapshot.Verdicts
	return review
}

// check walks the hunks of rule that have no reusable verdict in forks of at most hunksPerFork hunks. Each fork
// takes a slot of limit. A hunk that a reply skips leaves the rule with no verdict, and the next run asks only about it.
func (review ruleHunkReview) check(
	ctx context.Context, agent ReviewForkAgent, limit chan struct{}, settings ForkSettings, rule rulecatalog.ReviewRule,
) (RuleCheckResult, map[string]cachedHunkVerdict) {
	check := newRuleCheckResult(rule, settings)
	if len(review.hunks) == 0 {
		check.Verdict = RuleVerdictPass
		check.Checks = []RequirementCheck{{Requirement: rule.Text, Status: "N/A", Evidence: "no added line in the diff"}}
		return check, nil
	}
	keys := review.verdictKeys(rule, settings)
	verdicts := map[string]cachedHunkVerdict{}
	var open []int
	for index, key := range keys {
		verdict, found := review.cached[key]
		if found && notesKeepVerdict(review.run.config.RunContext, verdict.Notes, !isFailStatus(verdict.Status)) {
			verdicts[key] = verdict
			continue
		}
		open = append(open, index)
	}
	review.logger.Info("rule hunks reused", "rule", rule.ID, "hunks", len(keys), "reused", len(keys)-len(open))

	batches := slices.Collect(slices.Chunk(open, hunksPerFork))
	results := make([]hunkBatchResult, len(batches))
	var wait sync.WaitGroup
	for index, batch := range batches {
		wait.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			results[index] = review.checkHunks(ctx, agent, settings, rule, batch)
		})
	}
	wait.Wait()
	var errs []error
	for _, result := range results {
		check.Usage = AddUsage(check.Usage, result.usage)
		errs = append(errs, result.err)
		for index, verdict := range result.answered {
			verdicts[keys[index]] = verdict
		}
	}

	for index, hunk := range review.hunks {
		verdict, found := verdicts[keys[index]]
		if found {
			check.Checks = append(check.Checks, verdict.requirementCheck(hunk))
			check.Findings = append(check.Findings, verdict.ruleFindings(hunk)...)
		}
	}
	err := errors.Join(errs...)
	if err != nil {
		check.Error = err.Error()
		return check, verdicts
	}
	check.Verdict, err = ruleVerdictFromChecks(ruleForkReply{Checks: check.Checks})
	if err != nil {
		check.Error = err.Error()
	}
	return check, verdicts
}

// verdictKeys gives the cache key of each hunk for rule: the hunk file and its text, under the rule, the rendered
// prompt, the review context, the backend, and the fork settings. A text that more than one hunk of the file holds
// also keys on the hunk start, so those hunks never share a verdict and lose the reuse after a move.
func (review ruleHunkReview) verdictKeys(rule rulecatalog.ReviewRule, settings ForkSettings) []string {
	prompt := review.text.RuleHunkCheck(prompts.RuleHunkCheckInput{
		Hunks: []prompts.RuleHunk{{ID: "h1", File: "file", Header: "@@ -1 +1 @@"}},
	})
	ruleDigest := digest.SHA256Hex(strings.Join([]string{
		review.run.settingsDigest(prompt, settings), rule.ID, rule.Severity.String(), rule.Text,
	}, "\x00"))
	copies := map[string]int{}
	for _, hunk := range review.hunks {
		copies[hunk.File+"\x00"+hunk.Body]++
	}
	keys := make([]string, len(review.hunks))
	for index, hunk := range review.hunks {
		text := hunk.File + "\x00" + hunk.Body
		place := ""
		if copies[text] > 1 {
			place = strconv.Itoa(hunk.NewStart)
		}
		keys[index] = digest.SHA256Hex(strings.Join([]string{ruleDigest, text, place}, "\x00"))
	}
	return keys
}

// checkHunks runs one fork over the hunks at indexes and returns the verdict of each hunk the reply answers.
// An answer with a finding outside the added lines of its hunk is not taken, so the next run asks again.
func (review ruleHunkReview) checkHunks(
	ctx context.Context, agent ReviewForkAgent, settings ForkSettings, rule rulecatalog.ReviewRule, indexes []int,
) hunkBatchResult {
	input := prompts.RuleHunkCheckInput{
		RuleCheckInput: prompts.RuleCheckInput{ID: rule.ID, Severity: rule.Severity.String(), Text: rule.Text},
	}
	for _, index := range indexes {
		hunk := review.hunks[index]
		input.Hunks = append(input.Hunks, prompts.RuleHunk{ID: hunkID(index), File: hunk.File, Header: hunk.Header})
	}
	fork, err := agent.ForkParentSession(ctx, review.text.RuleHunkCheck(input), settings)
	if err != nil {
		return hunkBatchResult{usage: fork.Usage, err: fmt.Errorf("rule hunk fork failed: %w", err)}
	}
	var reply ruleHunkReply
	retryUsage, _, err := forkReplyJSON(ctx, agent, settings, fork, "hunks", &reply, review.text.JSONOnlyRetry)
	result := hunkBatchResult{usage: AddUsage(fork.Usage, retryUsage), answered: map[int]cachedHunkVerdict{}}
	if err != nil {
		result.err = fmt.Errorf("rule hunk fork reply: %w", err)
		return result
	}

	var unanswered []string
	for _, index := range indexes {
		hunk := review.hunks[index]
		position := slices.IndexFunc(reply.Hunks, func(answer ruleHunkAnswer) bool { return answer.Hunk == hunkID(index) })
		if position < 0 || strings.TrimSpace(reply.Hunks[position].Status) == "" {
			unanswered = append(unanswered, hunkID(index)+" "+hunk.File+" "+hunk.Header+" (no verdict)")
			continue
		}
		answer := reply.Hunks[position]
		if !answer.findingsInside(hunk) {
			unanswered = append(unanswered, hunkID(index)+" "+hunk.File+" "+hunk.Header+" (a finding outside its added lines)")
			continue
		}
		result.answered[index] = answer.cachedVerdict(hunk, review.run.config.RunContext)
	}
	if len(unanswered) > 0 {
		result.err = fmt.Errorf("rule hunk fork gave no usable verdict for %s", strings.Join(unanswered, "; "))
	}
	return result
}

// save keeps the verdicts of this run only, so the entry holds no hunk that left the branch. A run that can read
// uncommitted edits, at its start or now, stores nothing and leaves the entry as it was.
func (review ruleHunkReview) save(ctx context.Context, verdicts []map[string]cachedHunkVerdict) {
	if review.readsUncommittedFiles || review.canReadUncommittedFiles(ctx) {
		return
	}
	snapshot := ruleHunkSnapshot{Verdicts: map[string]cachedHunkVerdict{}}
	for _, ruleVerdicts := range verdicts {
		for key, verdict := range ruleVerdicts {
			snapshot.Verdicts[key] = verdict
		}
	}
	err := review.cache.save(ctx, review.stamp, snapshot)
	if err != nil {
		review.logger.Warn("rule hunk cache write failed", "error", err)
	}
}

// canReadUncommittedFiles reports whether a fork can read a file that no commit holds.
func (review ruleHunkReview) canReadUncommittedFiles(ctx context.Context) bool {
	return review.run.config.IncludeWorktree || hasUncommittedEdits(ctx, review.run.config.RepoRoot)
}

func hunkID(index int) string {
	return "h" + strconv.Itoa(index+1)
}

func isFailStatus(status string) bool {
	return strings.EqualFold(status, string(RuleVerdictFail))
}

// cachedHunkVerdict is the last verdict of one light rule on one hunk, and the --context notes it was judged under.
type cachedHunkVerdict struct {
	Status   string        `json:"status"`
	Evidence string        `json:"evidence"`
	Findings []hunkFinding `json:"findings"`
	Notes    []string      `json:"notes"`
}

// hunkFinding keeps its line as an offset from the hunk start, so a hunk that moved keeps its findings.
type hunkFinding struct {
	Offset  int    `json:"offset"`
	Message string `json:"message"`
}

// ruleHunkSnapshot is the cache entry: the verdict of each light rule on each hunk of the last run.
type ruleHunkSnapshot struct {
	Verdicts map[string]cachedHunkVerdict `json:"verdicts"`
}

// hunkBatchResult is the outcome of one fork over a batch of hunks, keyed by the hunk index.
type hunkBatchResult struct {
	answered map[int]cachedHunkVerdict
	usage    fantasy.Usage
	err      error
}

type ruleHunkReply struct {
	Hunks []ruleHunkAnswer `json:"hunks"`
}

type ruleHunkAnswer struct {
	Hunk     string        `json:"hunk"`
	Status   string        `json:"status"`
	Evidence string        `json:"evidence"`
	Findings []RuleFinding `json:"findings"`
}

// findingsInside reports whether every finding names the hunk's file and a line that the hunk adds.
func (answer ruleHunkAnswer) findingsInside(hunk DiffHunk) bool {
	for _, finding := range answer.Findings {
		if path.Clean(finding.File) != path.Clean(hunk.File) || !hunk.addsLine(finding.Line) {
			return false
		}
	}
	return true
}

func (answer ruleHunkAnswer) cachedVerdict(hunk DiffHunk, notes []string) cachedHunkVerdict {
	verdict := cachedHunkVerdict{Status: answer.Status, Evidence: answer.Evidence, Notes: notes}
	for _, finding := range answer.Findings {
		verdict.Findings = append(verdict.Findings, hunkFinding{Offset: finding.Line - hunk.NewStart, Message: finding.Message})
	}
	return verdict
}

func (verdict cachedHunkVerdict) requirementCheck(hunk DiffHunk) RequirementCheck {
	return RequirementCheck{Requirement: hunk.File + " " + hunk.Header, Status: verdict.Status, Evidence: verdict.Evidence}
}

func (verdict cachedHunkVerdict) ruleFindings(hunk DiffHunk) []RuleFinding {
	findings := make([]RuleFinding, 0, len(verdict.Findings))
	for _, finding := range verdict.Findings {
		findings = append(findings, RuleFinding{File: hunk.File, Line: hunk.NewStart + finding.Offset, Message: finding.Message})
	}
	return findings
}

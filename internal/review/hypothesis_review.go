package review

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/popoffvg/reviewrules/internal/digest"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

type hypothesisReview struct {
	config         ReviewRunConfig
	diff           ReviewDiff
	ignorePatterns []string
	contextDigest  string
}

// checkedHypotheses reads the branch's hypothesis cache, writes the questions, reuses each cached verdict
// that still holds, checks the other questions, and writes the questions and the verdicts back in one cache write.
func (review hypothesisReview) checkedHypotheses(
	ctx context.Context,
	logger *slog.Logger,
	agent ReviewForkAgent,
	limit chan struct{},
	generatorSettings ForkSettings,
	checkSettings ForkSettings,
	text prompts.Set,
) ([]HypothesisCheck, fantasy.Usage, error) {
	cache, err := branchHypothesisCache(ctx, review.config, review.diff, review.ignorePatterns)
	if err != nil {
		logger.Warn("hypothesis cache scope unavailable", "error", err)
	}
	previous, stamp, err := cache.load(ctx)
	if err != nil {
		logger.Warn("hypothesis cache read failed", "error", err)
	}
	snapshot, usage, generatorErr := review.hypotheses(ctx, logger, agent, limit, generatorSettings, text, previous)
	reuse := review.verdictReuse(ctx, checkSettings, text, previous.Verdicts)
	checks := checkHypotheses(ctx, logger, agent, limit, checkSettings, text, snapshot.Hypotheses, reuse)
	// A failed hunt group leaves the cache as it was, so the next run walks that group over the same delta again.
	if generatorErr != nil {
		return checks, usage, generatorErr
	}
	snapshot.Verdicts = reuse.verdictsToCache(checks)
	err = cache.save(ctx, stamp, snapshot)
	if err != nil {
		logger.Warn("hypothesis cache write failed", "error", err)
	}
	return checks, usage, nil
}

// hypotheses returns the snapshot of the questions to check: the cached one when the review inputs
// are unchanged, else the cached questions merged with the ones the generator forks write now.
func (review hypothesisReview) hypotheses(
	ctx context.Context,
	logger *slog.Logger,
	agent ReviewForkAgent,
	limit chan struct{},
	settings ForkSettings,
	text prompts.Set,
	previous hypothesisSnapshot,
) (hypothesisSnapshot, fantasy.Usage, error) {
	template := hypothesisGeneratorPrompt(text, prompts.HypothesisGeneratorInput{Hunts: allHypothesisHunts()})
	review.contextDigest = review.settingsDigest(template, settings)
	patchDigest := digest.SHA256Hex(review.diff.Patch)
	if previous.Head == review.diff.Head && previous.MergeBase == review.diff.MergeBase &&
		previous.ContextDigest == review.contextDigest && previous.PatchDigest == patchDigest && keepsNotes(review.config.RunContext, previous.Notes) {
		logger.Info("hypothesis cache reused unchanged diff", "hypotheses", len(previous.Hypotheses))
		return previous, fantasy.Usage{}, nil
	}

	input := prompts.HypothesisGeneratorInput{
		Delta:      review.delta(ctx, previous),
		Previous:   previousQuestions(previous.Hypotheses),
		AllowEmpty: true,
	}
	generated, usage, err := generateHypothesesByHuntGroup(ctx, agent, limit, settings, text, input)
	logger.Info("review hypothesis generator done", "hypotheses", len(generated), "usage", usage.String())
	return hypothesisSnapshot{
		Head: review.diff.Head, MergeBase: review.diff.MergeBase, PatchDigest: patchDigest,
		ContextDigest: review.contextDigest, Worktree: review.config.IncludeWorktree, Notes: review.config.RunContext,
		Hypotheses: mergeHypotheses(previous.Hypotheses, generated, review.diff.Head),
	}, usage, err
}

// checkHypotheses gives each hypothesis its cached verdict when that verdict still holds, and checks the others.
func checkHypotheses(
	ctx context.Context, logger *slog.Logger, agent ReviewForkAgent, limit chan struct{}, settings ForkSettings,
	text prompts.Set, hypotheses []Hypothesis, reuse verdictReuse,
) []HypothesisCheck {
	if len(hypotheses) == 0 {
		return nil
	}
	checks, open, openIndexes := reuse.splitByReuse(hypotheses)
	logger.Info("review hypothesis verdicts reused", "reused", len(hypotheses)-len(open), "to_check", len(open))
	fresh := checkAndRecheck(ctx, logger, agent, limit, settings, text, open)
	return mergeCheckedHypotheses(checks, fresh, openIndexes)
}

// mergeCheckedHypotheses puts each fresh check at its index among the reused checks, in the order of the hypotheses.
func mergeCheckedHypotheses(reused []HypothesisCheck, fresh []HypothesisCheck, freshIndexes []int) []HypothesisCheck {
	merged := slices.Clone(reused)
	for position, check := range fresh {
		merged[freshIndexes[position]] = check
	}
	return merged
}

// checkAndRecheck checks the hypotheses in ledger batches, then gives each UNSURE answer its own recheck fork.
func checkAndRecheck(
	ctx context.Context, logger *slog.Logger, agent ReviewForkAgent, limit chan struct{}, settings ForkSettings,
	text prompts.Set, hypotheses []Hypothesis,
) []HypothesisCheck {
	checks := checkHypothesisBatches(ctx, agent, limit, settings, hypotheses, text)
	logger.Info("review hypothesis ledger done", "hypotheses", len(checks))
	var recheck sync.WaitGroup
	for index := range checks {
		if checks[index].Verdict != HypothesisVerdictUnsure {
			continue
		}
		recheck.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			checks[index] = RecheckUndecidedHypothesis(ctx, agent, settings, checks[index], text)
			logger.Info("review hypothesis recheck done", "hypothesis", checks[index].ID, "verdict", checks[index].Verdict)
		})
	}
	recheck.Wait()
	return checks
}

func checkHypothesisBatches(
	ctx context.Context, agent ReviewForkAgent, limit chan struct{}, settings ForkSettings,
	hypotheses []Hypothesis, text prompts.Set,
) []HypothesisCheck {
	checks := make([]HypothesisCheck, len(hypotheses))
	var wait sync.WaitGroup
	for start := 0; start < len(hypotheses); start += maxHypotheses {
		end := min(start+maxHypotheses, len(hypotheses))
		wait.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			copy(checks[start:end], CheckHypothesesInFork(ctx, agent, settings, hypotheses[start:end], text))
		})
	}
	wait.Wait()
	return checks
}

func (review hypothesisReview) delta(ctx context.Context, previous hypothesisSnapshot) string {
	cfg := review.config
	if cfg.IncludeWorktree || previous.MergeBase != review.diff.MergeBase || previous.ContextDigest != review.contextDigest ||
		!keepsNotes(cfg.RunContext, previous.Notes) {
		return ""
	}
	pathspec := reviewPathspec(review.ignorePatterns)
	if previous.Worktree {
		committedPatch, err := loadReviewPatch(ctx, cfg.RepoRoot, []string{previous.MergeBase, previous.Head}, pathspec, cfg.MaxDiffBytes)
		if err != nil || digest.SHA256Hex(committedPatch) != previous.PatchDigest {
			return ""
		}
	}
	delta, err := loadReviewPatch(ctx, cfg.RepoRoot, []string{previous.Head, review.diff.Head}, pathspec, cfg.MaxDiffBytes)
	if err != nil {
		return ""
	}
	return delta
}

// keepsNotes reports whether every --context note that earlier work ran under is in given; a removed
// note can hide a question it suppressed or a finding it answered.
func keepsNotes(given []string, notes []string) bool {
	for _, note := range notes {
		if !slices.Contains(given, note) {
			return false
		}
	}
	return true
}

// settingsDigest keys cached work to the review context, the prompt template, the backend, and the fork settings.
func (review hypothesisReview) settingsDigest(template string, settings ForkSettings) string {
	return digest.SHA256Hex(strings.Join([]string{
		review.contextDigest, template, review.config.GeneratorBackend,
		settings.Model, settings.ReasoningEffort, strconv.Itoa(settings.MaxSteps),
	}, "\x00"))
}

func previousQuestions(hypotheses []Hypothesis) string {
	const contextBytes = 32 << 10
	var questions strings.Builder
	for i := len(hypotheses) - 1; i >= 0; i-- {
		hypothesis := hypotheses[i]
		item := fmt.Sprintf("- %s at %s: %s\n", hypothesis.Slug, hypothesis.At, hypothesis.Question)
		if questions.Len()+len(item) > contextBytes {
			break
		}
		questions.WriteString(item)
	}
	return strings.TrimSuffix(questions.String(), "\n")
}

func mergeHypotheses(previous []Hypothesis, generated []Hypothesis, commit string) []Hypothesis {
	merged := append([]Hypothesis(nil), previous...)
	slugs := make(map[string]bool, len(previous))
	questions := make(map[string]int, len(previous))
	for i, hypothesis := range previous {
		slugs[hypothesis.Slug] = true
		questions[hypothesisQuestionKey(hypothesis)] = i
	}
	for _, hypothesis := range generated {
		question := hypothesisQuestionKey(hypothesis)
		slug := hypothesisSlug(hypothesis.Slug, hypothesis.Question)
		if i, duplicate := questions[question]; duplicate {
			merged[i].Hunt = cmp.Or(hypothesis.Hunt, merged[i].Hunt)
			merged[i].Look = cmp.Or(hypothesis.Look, merged[i].Look)
			continue
		}
		base := slug
		for suffix := 2; slugs[slug]; suffix++ {
			slug = base + "-" + strconv.Itoa(suffix)
		}
		hypothesis.ID = fmt.Sprintf("H%d", len(merged)+1)
		hypothesis.Slug = slug
		hypothesis.Commit = commit
		slugs[slug] = true
		questions[question] = len(merged)
		merged = append(merged, hypothesis)
	}
	return merged
}

// hypothesisQuestionKey is the identity of a question across runs: its location and its text.
func hypothesisQuestionKey(hypothesis Hypothesis) string {
	return strings.TrimSpace(hypothesis.At) + "\x00" + strings.TrimSpace(hypothesis.Question)
}

func hypothesisSlug(slug string, question string) string {
	words := func(text string) []string {
		return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
			return (r < 'a' || r > 'z') && (r < '0' || r > '9')
		})
	}
	slugWords := words(slug)
	if len(slugWords) == 0 {
		slugWords = words(question)
	}
	if len(slugWords) == 0 {
		return digest.SHA256Hex(question)[:16]
	}
	return strings.Join(slugWords, "-")
}

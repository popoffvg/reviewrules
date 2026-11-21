package review

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"charm.land/fantasy"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

type hypothesisReview struct {
	config         ReviewRunConfig
	diff           ReviewDiff
	ignorePatterns []string
}

func (review hypothesisReview) hypotheses(
	ctx context.Context,
	logger *slog.Logger,
	agent ReviewForkAgent,
	limit chan struct{},
	settings ForkSettings,
	text prompts.Set,
) ([]Hypothesis, fantasy.Usage, error) {
	template := text.HypothesisGenerator(prompts.HypothesisGeneratorInput{MaxHypotheses: maxHypotheses})
	cache, err := branchHypothesisCache(ctx, review.config, review.diff, review.ignorePatterns, hypothesisDigest(template))
	if err != nil {
		logger.Warn("hypothesis cache scope unavailable", "error", err)
	}
	previous, stamp, err := cache.load(ctx)
	if err != nil {
		logger.Warn("hypothesis cache read failed", "error", err)
	}
	digest := hypothesisDigest(review.diff.Patch)
	if previous.MergeBase == review.diff.MergeBase && previous.PatchDigest == digest {
		logger.Info("hypothesis cache reused unchanged diff", "hypotheses", len(previous.Hypotheses))
		return previous.Hypotheses, fantasy.Usage{}, nil
	}

	input := prompts.HypothesisGeneratorInput{
		Delta:    review.delta(ctx, previous),
		Previous: previousQuestions(previous.Hypotheses),
	}
	limit <- struct{}{}
	generated, usage, err := GenerateHypothesesInFork(ctx, agent, settings, text, input)
	<-limit
	logger.Info("review hypothesis generator done", "hypotheses", len(generated), "usage", usage.String())
	if err != nil {
		return previous.Hypotheses, usage, err
	}
	hypotheses := mergeHypotheses(previous.Hypotheses, generated, review.diff.Head)
	err = cache.save(ctx, stamp, hypothesisSnapshot{
		Head: review.diff.Head, MergeBase: review.diff.MergeBase, PatchDigest: digest,
		Worktree: review.config.IncludeWorktree, Hypotheses: hypotheses,
	})
	if err != nil {
		logger.Warn("hypothesis cache write failed", "error", err)
	}
	return hypotheses, usage, nil
}

func (review hypothesisReview) delta(ctx context.Context, previous hypothesisSnapshot) string {
	if previous.MergeBase != review.diff.MergeBase {
		return ""
	}
	cfg := review.config
	pathspec := reviewPathspec(review.ignorePatterns)
	if previous.Worktree {
		committedPatch, err := loadReviewPatch(ctx, cfg.RepoRoot, []string{previous.MergeBase, previous.Head}, pathspec, cfg.MaxDiffBytes)
		if err != nil || hypothesisDigest(committedPatch) != previous.PatchDigest {
			return ""
		}
	}
	delta, err := loadReviewPatch(ctx, cfg.RepoRoot, reviewRevisions(previous.Head, review.diff.Head, cfg.IncludeWorktree), pathspec, cfg.MaxDiffBytes)
	if err != nil {
		return ""
	}
	return delta
}

func previousQuestions(hypotheses []Hypothesis) string {
	var questions strings.Builder
	for _, hypothesis := range hypotheses {
		fmt.Fprintf(&questions, "- %s: %s\n", hypothesis.Slug, hypothesis.Question)
	}
	return strings.TrimSuffix(questions.String(), "\n")
}

func mergeHypotheses(previous []Hypothesis, generated []Hypothesis, commit string) []Hypothesis {
	merged := append([]Hypothesis(nil), previous...)
	slugs := make(map[string]bool, len(previous))
	questions := make(map[string]bool, len(previous))
	questionKey := func(question string) string {
		return strings.ToLower(strings.Join(strings.Fields(question), " "))
	}
	for _, hypothesis := range previous {
		slugs[hypothesis.Slug] = true
		questions[questionKey(hypothesis.Question)] = true
	}
	for _, hypothesis := range generated {
		question := questionKey(hypothesis.Question)
		slug := hypothesisSlug(hypothesis.Slug, question)
		if slugs[slug] || questions[question] {
			continue
		}
		hypothesis.ID = fmt.Sprintf("H%d", len(merged)+1)
		hypothesis.Slug = slug
		hypothesis.Commit = commit
		merged = append(merged, hypothesis)
		slugs[slug] = true
		questions[question] = true
	}
	return merged
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
		return hypothesisDigest(question)[:16]
	}
	return strings.Join(slugWords, "-")
}

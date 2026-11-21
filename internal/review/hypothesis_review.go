package review

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"charm.land/fantasy"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

type hypothesisReview struct {
	config         ReviewRunConfig
	diff           ReviewDiff
	ignorePatterns []string
	contextDigest  string
}

func (review hypothesisReview) hypotheses(
	ctx context.Context,
	logger *slog.Logger,
	agent ReviewForkAgent,
	limit chan struct{},
	settings ForkSettings,
	text prompts.Set,
) ([]Hypothesis, fantasy.Usage, error) {
	template := hypothesisGeneratorPrompt(text, prompts.HypothesisGeneratorInput{})
	cache, err := branchHypothesisCache(ctx, review.config, review.diff, review.ignorePatterns, hypothesisDigest(template))
	if err != nil {
		logger.Warn("hypothesis cache scope unavailable", "error", err)
	}
	previous, stamp, err := cache.load(ctx)
	if err != nil {
		logger.Warn("hypothesis cache read failed", "error", err)
	}
	digest := hypothesisDigest(review.diff.Patch)
	if previous.MergeBase == review.diff.MergeBase && previous.ContextDigest == review.contextDigest && previous.PatchDigest == digest {
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
		ContextDigest: review.contextDigest, Worktree: review.config.IncludeWorktree, Hypotheses: hypotheses,
	})
	if err != nil {
		logger.Warn("hypothesis cache write failed", "error", err)
	}
	return hypotheses, usage, nil
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
	if previous.MergeBase != review.diff.MergeBase || previous.ContextDigest != review.contextDigest {
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
	questions := make(map[string]bool, len(previous))
	questionKey := func(hypothesis Hypothesis) string {
		question := strings.ToLower(strings.Join(strings.Fields(hypothesis.Question), " "))
		return strings.TrimSpace(hypothesis.At) + "\x00" + question
	}
	for _, hypothesis := range previous {
		slugs[hypothesis.Slug] = true
		questions[questionKey(hypothesis)] = true
	}
	for _, hypothesis := range generated {
		question := questionKey(hypothesis)
		slug := hypothesisSlug(hypothesis.Slug, hypothesis.Question)
		if questions[question] {
			continue
		}
		base := slug
		for suffix := 2; slugs[slug]; suffix++ {
			slug = base + "-" + strconv.Itoa(suffix)
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

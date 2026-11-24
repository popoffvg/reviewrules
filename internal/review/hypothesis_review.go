package review

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
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

func (review hypothesisReview) hypotheses(
	ctx context.Context,
	logger *slog.Logger,
	agent ReviewForkAgent,
	limit chan struct{},
	settings ForkSettings,
	text prompts.Set,
) ([]Hypothesis, fantasy.Usage, error) {
	template := hypothesisGeneratorPrompt(text, prompts.HypothesisGeneratorInput{})
	review.contextDigest = digest.SHA256Hex(strings.Join([]string{
		review.contextDigest, template, review.config.GeneratorBackend,
		settings.Model, settings.ReasoningEffort, strconv.Itoa(settings.MaxSteps),
	}, "\x00"))
	cache, err := branchHypothesisCache(ctx, review.config, review.diff, review.ignorePatterns)
	if err != nil {
		logger.Warn("hypothesis cache scope unavailable", "error", err)
	}
	previous, stamp, err := cache.load(ctx)
	if err != nil {
		logger.Warn("hypothesis cache read failed", "error", err)
	}
	patchDigest := digest.SHA256Hex(review.diff.Patch)
	if previous.Head == review.diff.Head && previous.MergeBase == review.diff.MergeBase &&
		previous.ContextDigest == review.contextDigest && previous.PatchDigest == patchDigest {
		logger.Info("hypothesis cache reused unchanged diff", "hypotheses", len(previous.Hypotheses))
		return previous.Hypotheses, fantasy.Usage{}, nil
	}

	input := prompts.HypothesisGeneratorInput{
		Delta:      review.delta(ctx, previous),
		Previous:   previousQuestions(previous.Hypotheses),
		AllowEmpty: true,
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
		Head: review.diff.Head, MergeBase: review.diff.MergeBase, PatchDigest: patchDigest,
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
	cfg := review.config
	if cfg.IncludeWorktree || previous.MergeBase != review.diff.MergeBase || previous.ContextDigest != review.contextDigest {
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
	questionKey := func(hypothesis Hypothesis) string {
		return strings.TrimSpace(hypothesis.At) + "\x00" + strings.TrimSpace(hypothesis.Question)
	}
	for i, hypothesis := range previous {
		slugs[hypothesis.Slug] = true
		questions[questionKey(hypothesis)] = i
	}
	for _, hypothesis := range generated {
		question := questionKey(hypothesis)
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

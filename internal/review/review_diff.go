package review

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ReviewDiff is the change under review. Patch is unified diff text with ignore patterns already excluded.
type ReviewDiff struct {
	Base         string
	MergeBase    string
	Head         string
	Patch        string
	ChangedFiles []string
}

// LoadReviewDiff diffs the merge base of base and HEAD against HEAD, or against the
// working tree when includeWorktree is set, so uncommitted edits are reviewed before a push.
func LoadReviewDiff(
	ctx context.Context,
	repoRoot string,
	base string,
	includeWorktree bool,
	ignorePatterns []string,
	maxBytes int,
) (ReviewDiff, error) {
	head, err := reviewHeadCommit(ctx, repoRoot)
	if err != nil {
		return ReviewDiff{}, err
	}
	mergeBase, err := runGit(ctx, repoRoot, "merge-base", base, head)
	if err != nil {
		return ReviewDiff{}, err
	}
	mergeBase = strings.TrimSpace(mergeBase)

	pathspec := reviewPathspec(ignorePatterns)
	revisions := reviewRevisions(mergeBase, head, includeWorktree)
	raw, err := runReviewDiff(ctx, repoRoot, revisions, pathspec, "--raw", "-z", "--patch")
	if err != nil {
		return ReviewDiff{}, err
	}
	if includeWorktree {
		currentHead, err := reviewHeadCommit(ctx, repoRoot)
		if err != nil {
			return ReviewDiff{}, err
		}
		if currentHead != head {
			return ReviewDiff{}, fmt.Errorf("review HEAD changed while reading the worktree diff: %s to %s", head, currentHead)
		}
	}
	names, patch := splitReviewDiff(raw)
	err = checkReviewPatchSize(patch, maxBytes)
	if err != nil {
		return ReviewDiff{}, err
	}
	return ReviewDiff{
		Base:         base,
		MergeBase:    mergeBase,
		Head:         head,
		Patch:        patch,
		ChangedFiles: names,
	}, nil
}

func reviewHeadCommit(ctx context.Context, repoRoot string) (string, error) {
	head, err := runGit(ctx, repoRoot, "rev-parse", "--verify", "HEAD")
	return strings.TrimSpace(head), err
}

func reviewRevisions(base string, head string, includeWorktree bool) []string {
	if includeWorktree {
		return []string{base}
	}
	return []string{base, head}
}

func reviewPathspec(ignorePatterns []string) []string {
	pathspec := []string{"--", "."}
	for _, pattern := range ignorePatterns {
		pathspec = append(pathspec, ":(exclude,glob)"+pattern)
	}
	return pathspec
}

func loadReviewPatch(ctx context.Context, repoRoot string, revisions []string, pathspec []string, maxBytes int) (string, error) {
	patch, err := runReviewDiff(ctx, repoRoot, revisions, pathspec)
	if err != nil {
		return "", err
	}
	return patch, checkReviewPatchSize(patch, maxBytes)
}

func runReviewDiff(ctx context.Context, repoRoot string, revisions []string, pathspec []string, flags ...string) (string, error) {
	args := append([]string{"diff", "--no-color", "--no-ext-diff"}, flags...)
	args = append(args, revisions...)
	return runGit(ctx, repoRoot, append(args, pathspec...)...)
}

func splitReviewDiff(raw string) ([]string, string) {
	records, patch, found := strings.Cut(raw, "\x00\x00")
	if !found {
		return nil, raw
	}
	fields := strings.Split(records, "\x00")
	var names []string
	for i := 0; i+1 < len(fields); i += 2 {
		status := fields[i][strings.LastIndexByte(fields[i], ' ')+1:]
		if i+2 < len(fields) && (strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C")) {
			i++
		}
		names = append(names, fields[i+1])
	}
	return names, patch
}

func checkReviewPatchSize(patch string, maxBytes int) error {
	if len(patch) > maxBytes {
		return fmt.Errorf(
			"review diff is %d bytes, over the max_diff_bytes limit of %d; add generated paths to ignorePatterns",
			len(patch),
			maxBytes,
		)
	}

	return nil
}

func runGit(ctx context.Context, repoRoot string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
	output, err := command.Output()
	if err != nil {
		stderr := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr = strings.TrimSpace(string(exitErr.Stderr))
		}
		return "", fmt.Errorf("git %s failed: %w: %s", strings.Join(args, " "), err, stderr)
	}
	return string(output), nil
}

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
	mergeBase, err := runGit(ctx, repoRoot, "merge-base", base, "HEAD")
	if err != nil {
		return ReviewDiff{}, err
	}
	mergeBase = strings.TrimSpace(mergeBase)

	revisions := []string{mergeBase, "HEAD"}
	if includeWorktree {
		revisions = []string{mergeBase}
	}
	pathspec := []string{"--", "."}
	for _, pattern := range ignorePatterns {
		pathspec = append(pathspec, ":(exclude,glob)"+pattern)
	}

	patchArgs := append(append([]string{"diff", "--no-color", "--no-ext-diff"}, revisions...), pathspec...)
	patch, err := runGit(ctx, repoRoot, patchArgs...)
	if err != nil {
		return ReviewDiff{}, err
	}
	if len(patch) > maxBytes {
		return ReviewDiff{}, fmt.Errorf(
			"review diff is %d bytes, over the max_diff_bytes limit of %d; add generated paths to ignorePatterns",
			len(patch),
			maxBytes,
		)
	}

	namesArgs := append(append([]string{"diff", "--name-only"}, revisions...), pathspec...)
	names, err := runGit(ctx, repoRoot, namesArgs...)
	if err != nil {
		return ReviewDiff{}, err
	}
	return ReviewDiff{
		Base:         base,
		Patch:        patch,
		ChangedFiles: strings.Fields(names),
	}, nil
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

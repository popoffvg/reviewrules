package review

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

type worktreeIndex struct {
	path string
	file *os.File
	info fs.FileInfo
}

func openWorktreeIndex(ctx context.Context, repoRoot string) (worktreeIndex, error) {
	path, err := runGit(ctx, repoRoot, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return worktreeIndex{}, err
	}
	index := worktreeIndex{path: strings.TrimSuffix(path, "\n")}
	file, err := os.Open(index.path) //nolint:gosec // Git returns the path of its own index
	if errors.Is(err, fs.ErrNotExist) {
		return index, nil
	}
	if err != nil {
		return worktreeIndex{}, fmt.Errorf("review worktree index open failed: %w", err)
	}
	index.file = file
	index.info, err = file.Stat()
	if err != nil {
		_ = file.Close()
		return worktreeIndex{}, fmt.Errorf("review worktree index stat failed: %w", err)
	}
	return index, nil
}

func (index worktreeIndex) checkUnchanged() error {
	current, err := os.Stat(index.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("review worktree index check failed: %w", err)
	}
	if index.info == nil && current == nil {
		return nil
	}
	// An open descriptor prevents reuse of the index's inode after Git replaces it.
	if current == nil || index.info == nil || !os.SameFile(index.info, current) ||
		index.info.Size() != current.Size() || !index.info.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf("review worktree index changed while reading the diff")
	}
	return nil
}

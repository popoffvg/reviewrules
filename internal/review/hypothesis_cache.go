package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/popoffvg/reviewrules/internal/digest"
)

const hypothesisCacheBytes = 200_000_000
const hypothesisCacheSuffix = ".hypotheses.json"

type hypothesisCache struct {
	dir      string
	key      string
	maxBytes int64
}

type hypothesisSnapshot struct {
	Head          string       `json:"head"`
	MergeBase     string       `json:"merge_base"`
	ContextDigest string       `json:"context_digest"`
	PatchDigest   string       `json:"patch_digest"`
	Worktree      bool         `json:"worktree"`
	Notes         []string     `json:"notes"`
	Hypotheses    []Hypothesis `json:"hypotheses"`
}

func branchHypothesisCache(ctx context.Context, cfg ReviewRunConfig, diff ReviewDiff, ignorePatterns []string) (hypothesisCache, error) {
	if cfg.HypothesisCacheDir == "" {
		return hypothesisCache{}, nil
	}
	branch, err := runGit(ctx, cfg.RepoRoot, "symbolic-ref", "--quiet", "HEAD")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return hypothesisCache{}, nil
	}
	if err != nil {
		return hypothesisCache{}, err
	}
	branch = strings.TrimSpace(branch)
	location, err := runGit(ctx, cfg.RepoRoot, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-prefix", branch)
	if err != nil {
		return hypothesisCache{}, err
	}
	fields := strings.SplitN(strings.TrimSuffix(location, "\n"), "\n", 3)
	if len(fields) != 3 || fields[2] != diff.Head {
		return hypothesisCache{}, nil
	}
	commonDir, prefix := fields[0], fields[1]
	commonDir, err = filepath.EvalSymlinks(commonDir)
	if err != nil {
		return hypothesisCache{}, err
	}
	scope := []string{commonDir, prefix, branch, cfg.Base}
	scope = append(scope, ignorePatterns...)
	return hypothesisCache{
		dir: cfg.HypothesisCacheDir, key: digest.SHA256Hex(strings.Join(scope, "\x00")),
		maxBytes: hypothesisCacheBytes,
	}, nil
}

func (cache hypothesisCache) load(ctx context.Context) (hypothesisSnapshot, string, error) {
	if cache.key == "" {
		return hypothesisSnapshot{}, "", nil
	}
	root, closeCache, err := cache.open(ctx)
	if err != nil {
		return hypothesisSnapshot{}, "", err
	}
	defer closeCache()
	data, stamp, err := cache.read(root)
	if err != nil || data == nil {
		return hypothesisSnapshot{}, stamp, err
	}
	var snapshot hypothesisSnapshot
	err = json.Unmarshal(data, &snapshot)
	if err != nil {
		return hypothesisSnapshot{}, stamp, fmt.Errorf("hypothesis cache entry decode failed: %w", err)
	}
	if !isGitCommit(snapshot.Head) || !isGitCommit(snapshot.MergeBase) {
		return hypothesisSnapshot{}, stamp, fmt.Errorf("hypothesis cache entry has an invalid commit")
	}
	now := time.Now()
	err = root.Chtimes(cache.fileName(), now, now)
	return snapshot, stamp, err
}

func (cache hypothesisCache) save(ctx context.Context, expected string, snapshot hypothesisSnapshot) error {
	if cache.key == "" {
		return nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	root, closeCache, err := cache.open(ctx)
	if err != nil {
		return err
	}
	defer closeCache()
	_, current, err := cache.read(root)
	if err != nil || current != expected {
		return err
	}
	if int64(len(data)) > cache.maxBytes {
		var removeErr error
		if current != "" {
			removeErr = root.Remove(cache.fileName())
		}
		return errors.Join(fmt.Errorf("hypothesis snapshot exceeds the cache budget: %d bytes", len(data)), removeErr)
	}
	const pending = ".hypotheses-pending"
	err = root.Remove(pending)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	file, err := root.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(pending) }()
	_, writeErr := file.Write(data)
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		return err
	}
	err = cache.evict(root, int64(len(data)))
	if err != nil {
		return err
	}
	return root.Rename(pending, cache.fileName())
}

func (cache hypothesisCache) open(ctx context.Context) (*os.Root, func(), error) {
	err := os.MkdirAll(cache.dir, 0o700)
	if err != nil {
		return nil, nil, err
	}
	lock := flock.New(filepath.Join(cache.dir, ".hypotheses-lock"))
	lockContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(lockContext, 25*time.Millisecond)
	if err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("hypothesis cache lock failed: %w", err)
	}
	if !locked {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("hypothesis cache lock unavailable")
	}
	root, err := os.OpenRoot(cache.dir)
	if err != nil {
		_ = lock.Close()
		return nil, nil, err
	}
	return root, func() {
		_ = root.Close()
		_ = lock.Close()
	}, nil
}

func (cache hypothesisCache) read(root *os.Root) ([]byte, string, error) {
	name := cache.fileName()
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("hypothesis cache entry is not a regular file")
	}
	if info.Size() > cache.maxBytes {
		return nil, "oversize", nil
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, "", err
	}
	stamp := digest.SHA256Hex(string(data))
	return data, stamp, nil
}

func (cache hypothesisCache) evict(root *os.Root, incoming int64) error {
	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	var entries []fs.FileInfo
	total := incoming
	for _, file := range files {
		key, entry := strings.CutSuffix(file.Name(), hypothesisCacheSuffix)
		if !entry || len(key) != sha256.Size*2 || file.Name() == cache.fileName() {
			continue
		}
		if _, err := hex.DecodeString(key); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			entries = append(entries, info)
			total += info.Size()
		}
	}
	slices.SortStableFunc(entries, func(a, b fs.FileInfo) int { return a.ModTime().Compare(b.ModTime()) })
	for _, entry := range entries {
		if total <= cache.maxBytes {
			break
		}
		err = root.Remove(entry.Name())
		if err != nil {
			return err
		}
		total -= entry.Size()
	}
	return nil
}

func (cache hypothesisCache) fileName() string {
	return cache.key + hypothesisCacheSuffix
}

func isGitCommit(commit string) bool {
	_, err := hex.DecodeString(commit)
	return err == nil && (len(commit) == 40 || len(commit) == 64)
}

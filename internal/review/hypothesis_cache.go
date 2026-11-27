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

// branchCache is one JSON cache entry of a branch; suffix names its kind, such as hypothesisCacheSuffix.
type branchCache struct {
	dir      string
	key      string
	suffix   string
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
	// Verdicts maps the question key of each checked hypothesis to its last verdict.
	Verdicts map[string]cachedVerdict `json:"verdicts,omitempty"`
}

func branchHypothesisCache(ctx context.Context, cfg ReviewRunConfig, diff ReviewDiff, ignorePatterns []string) (branchCache, error) {
	return branchCacheEntry(ctx, cfg, diff, ignorePatterns, hypothesisCacheSuffix)
}

// branchCacheEntry scopes the entry to the repo, the branch, and the base. A detached HEAD has no entry.
func branchCacheEntry(
	ctx context.Context, cfg ReviewRunConfig, diff ReviewDiff, ignorePatterns []string, suffix string,
) (branchCache, error) {
	if cfg.HypothesisCacheDir == "" {
		return branchCache{}, nil
	}
	branch, err := runGit(ctx, cfg.RepoRoot, "symbolic-ref", "--quiet", "HEAD")
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return branchCache{}, nil
	}
	if err != nil {
		return branchCache{}, fmt.Errorf("cache scope: read the branch of %s: %w", cfg.RepoRoot, err)
	}
	branch = strings.TrimSpace(branch)
	location, err := runGit(ctx, cfg.RepoRoot, "rev-parse", "--path-format=absolute", "--git-common-dir", "--show-prefix", branch)
	if err != nil {
		return branchCache{}, fmt.Errorf("cache scope: locate branch %s: %w", branch, err)
	}
	fields := strings.SplitN(strings.TrimSuffix(location, "\n"), "\n", 3)
	if len(fields) != 3 || fields[2] != diff.Head {
		return branchCache{}, nil
	}
	commonDir, prefix := fields[0], fields[1]
	commonDir, err = filepath.EvalSymlinks(commonDir)
	if err != nil {
		return branchCache{}, fmt.Errorf("cache scope: resolve %s: %w", commonDir, err)
	}
	scope := []string{commonDir, prefix, branch, cfg.Base}
	scope = append(scope, ignorePatterns...)
	return branchCache{
		dir: cfg.HypothesisCacheDir, key: digest.SHA256Hex(strings.Join(scope, "\x00")),
		suffix: suffix, maxBytes: hypothesisCacheBytes,
	}, nil
}

func (cache branchCache) load(ctx context.Context) (hypothesisSnapshot, string, error) {
	var snapshot hypothesisSnapshot
	found, stamp, err := cache.loadJSON(ctx, &snapshot)
	if err != nil || !found {
		return hypothesisSnapshot{}, stamp, err
	}
	if !isGitCommit(snapshot.Head) || !isGitCommit(snapshot.MergeBase) {
		return hypothesisSnapshot{}, stamp, fmt.Errorf("hypothesis cache entry has an invalid commit")
	}
	return snapshot, stamp, nil
}

// loadJSON decodes the entry into target and marks it used. found is false when the entry is absent or over budget.
func (cache branchCache) loadJSON(ctx context.Context, target any) (bool, string, error) {
	if cache.key == "" {
		return false, "", nil
	}
	found, stamp, err := cache.decode(ctx, target)
	if err != nil {
		return false, stamp, fmt.Errorf("load cache entry %s in %s: %w", cache.fileName(), cache.dir, err)
	}
	return found, stamp, nil
}

func (cache branchCache) decode(ctx context.Context, target any) (bool, string, error) {
	root, closeCache, err := cache.open(ctx)
	if err != nil {
		return false, "", err
	}
	defer closeCache()
	data, stamp, err := cache.read(root)
	if err != nil || data == nil {
		return false, stamp, err
	}
	err = json.Unmarshal(data, target)
	if err != nil {
		return false, stamp, fmt.Errorf("decode: %w", err)
	}
	now := time.Now()
	err = root.Chtimes(cache.fileName(), now, now)
	if err != nil {
		return false, stamp, fmt.Errorf("mark used: %w", err)
	}
	return true, stamp, nil
}

// save writes entry when the stored entry still has the stamp expected, so a run never overwrites a newer run.
func (cache branchCache) save(ctx context.Context, expected string, entry any) error {
	if cache.key == "" {
		return nil
	}
	err := cache.write(ctx, expected, entry)
	if err != nil {
		return fmt.Errorf("save cache entry %s in %s: %w", cache.fileName(), cache.dir, err)
	}
	return nil
}

func (cache branchCache) write(ctx context.Context, expected string, entry any) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
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
		return errors.Join(fmt.Errorf("entry exceeds the cache budget: %d bytes", len(data)), removeErr)
	}
	const pending = ".branch-cache-pending"
	err = root.Remove(pending)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove the old %s: %w", pending, err)
	}
	file, err := root.OpenFile(pending, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create %s: %w", pending, err)
	}
	defer func() { _ = root.Remove(pending) }()
	_, writeErr := file.Write(data)
	err = errors.Join(writeErr, file.Close())
	if err != nil {
		return fmt.Errorf("write %s: %w", pending, err)
	}
	err = cache.evict(root, int64(len(data)))
	if err != nil {
		return fmt.Errorf("evict old entries: %w", err)
	}
	err = root.Rename(pending, cache.fileName())
	if err != nil {
		return fmt.Errorf("rename %s to the entry: %w", pending, err)
	}
	return nil
}

func (cache branchCache) open(ctx context.Context) (*os.Root, func(), error) {
	err := os.MkdirAll(cache.dir, 0o700)
	if err != nil {
		return nil, nil, fmt.Errorf("create the cache dir: %w", err)
	}
	lock := flock.New(filepath.Join(cache.dir, ".branch-cache-lock"))
	lockContext, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	locked, err := lock.TryLockContext(lockContext, 25*time.Millisecond)
	if err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("lock the cache dir: %w", err)
	}
	if !locked {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("lock the cache dir: another run holds the lock")
	}
	root, err := os.OpenRoot(cache.dir)
	if err != nil {
		_ = lock.Close()
		return nil, nil, fmt.Errorf("open the cache dir: %w", err)
	}
	return root, func() {
		_ = root.Close()
		_ = lock.Close()
	}, nil
}

func (cache branchCache) read(root *os.Root) ([]byte, string, error) {
	name := cache.fileName()
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", fmt.Errorf("stat the entry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("the entry is not a regular file")
	}
	if info.Size() > cache.maxBytes {
		return nil, "oversize", nil
	}
	data, err := root.ReadFile(name)
	if err != nil {
		return nil, "", fmt.Errorf("read the entry: %w", err)
	}
	stamp := digest.SHA256Hex(string(data))
	return data, stamp, nil
}

func (cache branchCache) evict(root *os.Root, incoming int64) error {
	files, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return fmt.Errorf("list %s: %w", cache.dir, err)
	}
	var entries []fs.FileInfo
	total := incoming
	for _, file := range files {
		key, entry := strings.CutSuffix(file.Name(), cache.suffix)
		if !entry || len(key) != sha256.Size*2 || file.Name() == cache.fileName() {
			continue
		}
		if _, err := hex.DecodeString(key); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return fmt.Errorf("stat %s: %w", file.Name(), err)
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
			return fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
		total -= entry.Size()
	}
	return nil
}

func (cache branchCache) fileName() string {
	return cache.key + cache.suffix
}

func isGitCommit(commit string) bool {
	_, err := hex.DecodeString(commit)
	return err == nil && (len(commit) == 40 || len(commit) == 64)
}

package ruleset

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// RuleSetCheckout is a rule set checked out at one commit in the cache.
type RuleSetCheckout struct {
	RuleSet
	Commit string
	// Dir holds the files of the rule set repo at Commit.
	Dir string
}

// fullCommitPattern matches a ref that is a full commit sha. Such a ref never moves, so a cached checkout of it needs no fetch.
var fullCommitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// RuleSetCacheDir is $XDG_CACHE_HOME/reviewrules/rule-sets, else the same path under os.UserCacheDir.
func RuleSetCacheDir() (string, error) {
	root := os.Getenv("XDG_CACHE_HOME")
	if root == "" {
		var err error
		root, err = os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("rule set cache dir unknown: %w", err)
		}
	}
	return filepath.Join(root, "reviewrules", "rule-sets"), nil
}

// FetchRuleSets fetches every rule set in list order and stops at the first that has no usable checkout.
func FetchRuleSets(ctx context.Context, logger *slog.Logger, cacheDir string, sets RuleSetList) ([]RuleSetCheckout, error) {
	checkouts := make([]RuleSetCheckout, 0, len(sets))
	for _, set := range sets {
		checkout, err := FetchRuleSet(ctx, logger, cacheDir, set)
		if err != nil {
			return nil, err
		}
		logger.Info("rule set ready", "rule_set", set.Name, "ref", set.Ref, "commit", checkout.Commit)
		checkouts = append(checkouts, checkout)
	}
	return checkouts, nil
}

// FetchRuleSet fetches set.Ref with git and checks out its commit in the cache. A full sha ref
// with a cached checkout uses no network. When the fetch fails, the last cached checkout of the ref
// is used with a warning.
func FetchRuleSet(ctx context.Context, logger *slog.Logger, cacheDir string, set RuleSet) (RuleSetCheckout, error) {
	cache := newRuleSetCache(cacheDir, set.URL)
	if fullCommitPattern.MatchString(set.Ref) && cache.hasCheckout(set.Ref) {
		return cache.checkout(set, set.Ref), nil
	}
	commit, err := cache.fetchCommit(ctx, set.URL, set.Ref)
	if err != nil {
		cached, found := cache.lastCommit(set.Ref)
		if !found {
			return RuleSetCheckout{}, fmt.Errorf("rule set fetch failed: rule set %q from %s: %w", set.Name, set.URL, err)
		}
		logger.Warn("rule set fetch failed; the review uses the cached checkout", "rule_set", set.Name, "url", set.URL, "commit", cached, "error", err)
		return cache.checkout(set, cached), nil
	}
	err = cache.writeCheckout(ctx, commit)
	if err != nil {
		return RuleSetCheckout{}, fmt.Errorf("rule set checkout failed: rule set %q commit %s: %w", set.Name, commit, err)
	}
	return cache.checkout(set, commit), nil
}

// CachedRuleSetCommit is the commit of the last checkout of set.Ref in the cache, or "" when there is none.
func CachedRuleSetCommit(cacheDir string, set RuleSet) string {
	commit, _ := newRuleSetCache(cacheDir, set.URL).lastCommit(set.Ref)
	return commit
}

// ruleSetCache is the cache folder of one rule set URL: a bare git repo in git/, one checkout per
// commit in commits/<sha>/, and one file per tag or branch in refs/ that names its last commit.
type ruleSetCache struct {
	dir string
}

func newRuleSetCache(cacheDir string, url string) ruleSetCache {
	return ruleSetCache{dir: filepath.Join(cacheDir, sha256Hex(url))}
}

func sha256Hex(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

func (c ruleSetCache) gitDir() string { return filepath.Join(c.dir, "git") }

func (c ruleSetCache) commitDir(commit string) string { return filepath.Join(c.dir, "commits", commit) }

// refFile hashes the ref, so a ref with '/' or '..' stays one file name.
func (c ruleSetCache) refFile(ref string) string { return filepath.Join(c.dir, "refs", sha256Hex(ref)) }

func (c ruleSetCache) hasCheckout(commit string) bool {
	info, err := os.Stat(c.commitDir(commit))
	return err == nil && info.IsDir()
}

func (c ruleSetCache) checkout(set RuleSet, commit string) RuleSetCheckout {
	return RuleSetCheckout{RuleSet: set, Commit: commit, Dir: c.commitDir(commit)}
}

func (c ruleSetCache) lastCommit(ref string) (string, bool) {
	commit := ref
	if !fullCommitPattern.MatchString(ref) {
		raw, err := os.ReadFile(c.refFile(ref))
		if err != nil {
			return "", false
		}
		commit = strings.TrimSpace(string(raw))
	}
	if !fullCommitPattern.MatchString(commit) || !c.hasCheckout(commit) {
		return "", false
	}
	return commit, true
}

// fetchCommit fetches one commit with depth 1 and records it as the last commit of ref.
func (c ruleSetCache) fetchCommit(ctx context.Context, url string, ref string) (string, error) {
	err := os.MkdirAll(filepath.Join(c.dir, "refs"), 0o750)
	if err != nil {
		return "", err
	}
	_, err = runGit(ctx, nil, "init", "--quiet", "--bare", c.gitDir())
	if err != nil {
		return "", err
	}
	_, err = runGit(ctx, nil, "--git-dir", c.gitDir(), "fetch", "--quiet", "--depth", "1", "--no-tags", "--end-of-options", url, ref)
	if err != nil {
		return "", err
	}
	output, err := runGit(ctx, nil, "--git-dir", c.gitDir(), "rev-parse", "--verify", "FETCH_HEAD^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(output)
	if !fullCommitPattern.MatchString(ref) {
		err = os.WriteFile(c.refFile(ref), []byte(commit+"\n"), 0o600)
		if err != nil {
			return "", err
		}
	}
	return commit, nil
}

// writeCheckout writes the files of commit to a temp folder, then renames it, so a commit folder is always complete.
func (c ruleSetCache) writeCheckout(ctx context.Context, commit string) error {
	if c.hasCheckout(commit) {
		return nil
	}
	commitsDir := filepath.Dir(c.commitDir(commit))
	err := os.MkdirAll(commitsDir, 0o750)
	if err != nil {
		return err
	}
	temp, err := os.MkdirTemp(commitsDir, commit+".partial-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(temp) }()
	indexFile := temp + ".index"
	defer func() { _ = os.Remove(indexFile) }()
	env := []string{"GIT_INDEX_FILE=" + indexFile}
	_, err = runGit(ctx, env, "--git-dir", c.gitDir(), "read-tree", commit)
	if err != nil {
		return err
	}
	_, err = runGit(ctx, env, "--git-dir", c.gitDir(), "--work-tree", temp, "checkout-index", "--all")
	if err != nil {
		return err
	}
	err = os.Rename(temp, c.commitDir(commit))
	if err != nil && c.hasCheckout(commit) {
		// Another run wrote the same commit first.
		return nil
	}
	return err
}

// runGit runs git with the user's own git config and credentials. GIT_TERMINAL_PROMPT=0 makes a
// fetch that needs a password fail instead of waiting for input.
func runGit(ctx context.Context, env []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Env = append(append(os.Environ(), "GIT_TERMINAL_PROMPT=0"), env...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

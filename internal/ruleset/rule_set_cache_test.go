package ruleset

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRuleSetRepo makes a git repo with a root .reviewrules/config.json, tags its commit v1, and
// returns its file:// URL, its folder, and the commit sha.
func newRuleSetRepo(t *testing.T) (url string, dir string, commit string) {
	t.Helper()
	_, err := exec.LookPath("git")
	if err != nil {
		t.Skip("rule set tests need the git binary on PATH: " + err.Error())
	}
	dir = t.TempDir()
	err = os.MkdirAll(filepath.Join(dir, ".reviewrules"), 0o750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(dir, ".reviewrules", "config.json"), []byte(`{"rules":[{"id":"r","rule":"x"}]}`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	gitInRepo(t, dir, "init", "-q")
	gitInRepo(t, dir, "add", "-A")
	gitInRepo(t, dir, "commit", "-q", "-m", "rules")
	gitInRepo(t, dir, "tag", "v1")
	return "file://" + filepath.ToSlash(dir), dir, strings.TrimSpace(gitInRepo(t, dir, "rev-parse", "HEAD"))
}

func gitInRepo(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.hooksPath=" + os.DevNull}
	output, err := exec.CommandContext(t.Context(), "git", append(base, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}

func newWarnLogger() (*slog.Logger, *bytes.Buffer) {
	var out bytes.Buffer
	return slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelWarn})), &out
}

func TestFetchRuleSetShaRefOfflineFromCache(t *testing.T) {
	url, source, commit := newRuleSetRepo(t)
	cacheDir := t.TempDir()
	logger, _ := newWarnLogger()

	tagged, err := FetchRuleSet(t.Context(), logger, cacheDir, RuleSet{Name: "go", URL: url, Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if tagged.Commit != commit {
		t.Fatalf("commit = %s, want %s", tagged.Commit, commit)
	}
	_, err = os.Stat(filepath.Join(tagged.Dir, ".reviewrules", "config.json"))
	if err != nil {
		t.Fatalf("checkout has no config.json: %v", err)
	}

	err = os.RemoveAll(source)
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := FetchRuleSet(t.Context(), logger, cacheDir, RuleSet{Name: "go", URL: url, Ref: commit})
	if err != nil {
		t.Fatalf("sha ref with a cached checkout needs no network: %v", err)
	}
	if pinned.Dir != tagged.Dir {
		t.Errorf("dir = %s, want the cached %s", pinned.Dir, tagged.Dir)
	}
}

func TestFetchRuleSetFailureUsesCachedCheckout(t *testing.T) {
	url, source, commit := newRuleSetRepo(t)
	cacheDir := t.TempDir()
	logger, warnings := newWarnLogger()
	set := RuleSet{Name: "go", URL: url, Ref: "v1"}
	_, err := FetchRuleSet(t.Context(), logger, cacheDir, set)
	if err != nil {
		t.Fatal(err)
	}
	err = os.RemoveAll(source)
	if err != nil {
		t.Fatal(err)
	}

	checkout, err := FetchRuleSet(t.Context(), logger, cacheDir, set)
	if err != nil {
		t.Fatalf("fetch failure with a cached checkout: %v", err)
	}
	if checkout.Commit != commit {
		t.Errorf("commit = %s, want the cached %s", checkout.Commit, commit)
	}
	if !strings.Contains(warnings.String(), "rule set fetch failed") || !strings.Contains(warnings.String(), "rule_set=go") {
		t.Errorf("warning = %q; want one that names the rule set", warnings.String())
	}
	if got := CachedRuleSetCommit(cacheDir, set); got != commit {
		t.Errorf("cached commit = %q, want %s", got, commit)
	}
}

func TestFetchRuleSetFailureWithoutCache(t *testing.T) {
	url, source, _ := newRuleSetRepo(t)
	err := os.RemoveAll(source)
	if err != nil {
		t.Fatal(err)
	}
	logger, _ := newWarnLogger()
	_, err = FetchRuleSet(t.Context(), logger, t.TempDir(), RuleSet{Name: "go", URL: url, Ref: "v1"})
	if err == nil {
		t.Fatal("fetch of a missing repo with no cache gave no error")
	}
	for _, want := range []string{"rule set fetch failed", `"go"`, url, "fatal:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v; want it to contain %q", err, want)
		}
	}
}

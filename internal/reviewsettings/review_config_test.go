package reviewsettings

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/review"
)

const testUserConfig = `
[local]
agent = "codex"
model = "gpt-6-luna"
parallel = 4

[local.prices."gpt-6-luna"]
input = 1.0
cached = 0.5
output = 2.0

[ci]
agent = "fantasy"
base_url = "http://10.0.0.10:4000/v1"
`

func writeConfigFile(t testing.TB, path string, content string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o750)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoadReviewConfigUserCases(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.toml")
	writeConfigFile(t, userPath, testUserConfig)
	repo := t.TempDir()

	local, err := LoadReviewConfig(userPath, UserConfigCaseLocal, repo)
	if err != nil {
		t.Fatal(err)
	}
	if local.User.Agent != ReviewAgentKindCodex || local.User.Parallel != 4 || local.User.HypothesisModel != "gpt-6.1-sol" {
		t.Errorf("local case = %+v; want codex, parallel 4, default hypothesis model", local.User)
	}
	if got := local.User.Prices["gpt-6-luna"]; got != (review.ModelTokenPrice{Input: 1, CachedInput: 0.5, Output: 2}) {
		t.Errorf("gpt-6-luna price = %+v; want the table entry", got)
	}
	if got := local.User.Prices["gpt-6.1-sol"]; got != review.DefaultModelTokenPrices()["gpt-6.1-sol"] {
		t.Errorf("gpt-6.1-sol price = %+v; want the built-in price", got)
	}

	ci, err := LoadReviewConfig(userPath, UserConfigCaseCI, repo)
	if err != nil {
		t.Fatal(err)
	}
	if ci.User.Agent != ReviewAgentKindFantasy || ci.User.BaseURL != "http://10.0.0.10:4000/v1" || ci.User.Parallel != 8 {
		t.Errorf("ci case = %+v; want fantasy, the proxy URL, default parallel 8", ci.User)
	}
	if !reflect.DeepEqual(ci.User.Prices, review.DefaultModelTokenPrices()) {
		t.Errorf("ci prices = %+v; want the built-in prices", ci.User.Prices)
	}
}

func TestLoadReviewConfigMissingFilesGiveDefaults(t *testing.T) {
	config, err := LoadReviewConfig(filepath.Join(t.TempDir(), "none.toml"), UserConfigCaseLocal, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.User, DefaultUserConfig()) || !reflect.DeepEqual(config.Repo, DefaultRepoSettings()) {
		t.Fatalf("config = %+v; want defaults", config)
	}
}

func TestLoadReviewConfigRepoSettings(t *testing.T) {
	repo := t.TempDir()
	writeConfigFile(t, filepath.Join(repo, ".reviewrules", "settings.toml"), `
rule_folders = [".team-rules"]
fail_on = "high"
correctness = false
`)
	config, err := LoadReviewConfig(filepath.Join(t.TempDir(), "none.toml"), UserConfigCaseLocal, repo)
	if err != nil {
		t.Fatal(err)
	}
	want := RepoSettings{RuleFolders: []string{".team-rules"}, FailOn: "high", Correctness: false, MaxDiffBytes: 300_000}
	if !reflect.DeepEqual(config.Repo, want) {
		t.Fatalf("repo settings = %+v; want %+v", config.Repo, want)
	}
}

func TestLoadReviewConfigRejectsBadKeys(t *testing.T) {
	tests := []struct {
		name       string
		repo       string
		user       string
		wantErr    string
		wantSource string
	}{
		{name: "repo-unknown-key", repo: `rule_folder = [".x"]`, wantErr: `config key unknown: "rule_folder"`},
		{name: "repo-user-key", repo: `agent = "codex"`, wantErr: `config key misplaced: "agent"`, wantSource: "belongs in ~/.config/reviewrules/config.toml"},
		{name: "user-repo-key", user: "[ci]\nfail_on = \"low\"", wantErr: `config key misplaced: "fail_on"`, wantSource: "belongs in .reviewrules/settings.toml"},
		{name: "user-unknown-key", user: "[local]\nagnet = \"x\"", wantErr: `config key unknown: "local.agnet"`},
		{name: "user-unknown-price-field", user: "[local.prices.x]\ninput_usd = 1.0", wantErr: `config key unknown: "local.prices.x.input_usd"`},
		{name: "path-separator", repo: `rule_folders = ["a/b"]`, wantErr: "bad rule folder name"},
		{name: "backslash", repo: `rule_folders = ["a\\b"]`, wantErr: "bad rule folder name"},
		{name: "parent-dir", repo: `rule_folders = [".."]`, wantErr: "bad rule folder name"},
		{name: "empty-name", repo: `rule_folders = [""]`, wantErr: "bad rule folder name"},
		{name: "empty-agent", user: "[local]\nagent = \"\"", wantErr: "user config agent is empty"},
		{name: "rule-set-unknown-field", repo: "[[rule_sets]]\nname = \"go\"\nurl = \"file:///x\"\nref = \"v1\"\nbranch = \"main\"", wantErr: `rule set entry invalid: [[rule_sets]] entry 1 (name "go"): unknown field "branch"`, wantSource: "settings.toml"},
		{name: "rule-set-missing-ref", repo: "[[rule_sets]]\nname = \"go\"\nurl = \"file:///x\"", wantErr: `rule set entry invalid: [[rule_sets]] entry 1 (name "go"): field "ref" missing`, wantSource: "settings.toml"},
		{name: "rule-set-bad-name", repo: "[[rule_sets]]\nname = \"Go_Rules\"\nurl = \"file:///x\"\nref = \"v1\"", wantErr: `field "name": value "Go_Rules" MUST match [a-z0-9-]+`},
		{name: "rule-set-duplicate-name", repo: "[[rule_sets]]\nname = \"go\"\nurl = \"file:///a\"\nref = \"v1\"\n[[rule_sets]]\nname = \"go\"\nurl = \"file:///b\"\nref = \"v2\"", wantErr: `rule set name duplicate: [[rule_sets]] entries 1 and 2 both have name "go"`, wantSource: "settings.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := t.TempDir()
			userPath := filepath.Join(t.TempDir(), "config.toml")
			if tt.repo != "" {
				writeConfigFile(t, filepath.Join(repo, ".reviewrules", "settings.toml"), tt.repo)
			}
			if tt.user != "" {
				writeConfigFile(t, userPath, tt.user)
			}
			_, err := LoadReviewConfig(userPath, UserConfigCaseLocal, repo)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.Contains(err.Error(), tt.wantSource) {
				t.Fatalf("err = %v, want it to contain %q and %q", err, tt.wantErr, tt.wantSource)
			}
		})
	}
}

func TestLoadReviewConfigAcceptsExecPluginAgent(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.toml")
	writeConfigFile(t, userPath, "[local]\nagent = \"gemini\"")
	config, err := LoadReviewConfig(userPath, UserConfigCaseLocal, t.TempDir())
	if err != nil || config.User.Agent != "gemini" {
		t.Fatalf("config = %+v, err %v; want exec plugin agent gemini", config.User, err)
	}
}

func TestWriteMergedConfigNamesEachSource(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.toml")
	writeConfigFile(t, userPath, testUserConfig)
	repo := t.TempDir()
	repoPath := filepath.Join(repo, ".reviewrules", "settings.toml")
	writeConfigFile(t, repoPath, `fail_on = "low"`)
	config, err := LoadReviewConfig(userPath, UserConfigCaseLocal, repo)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = config.WriteMergedConfig(&out, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		name, _, _ := strings.Cut(line, " ")
		lines[name] = line
	}
	userSource := "# " + userPath + " [local]"
	for name, want := range map[string]string{
		"fail_on":              "# " + repoPath,
		"correctness":          "# default",
		"agent":                userSource,
		"model":                userSource,
		"base_url":             "# default",
		`prices.gpt-6-luna`:    userSource,
		`prices."gpt-6.1-sol"`: "# default",
	} {
		if !strings.HasSuffix(lines[name], want) {
			t.Errorf("line for %s = %q; want it to end with %q", name, lines[name], want)
		}
	}
	if !strings.Contains(lines["prices.gpt-6-luna"], "{input = 1.0, cached = 0.5, output = 2.0}") {
		t.Errorf("price line = %q; want the inline table", lines["prices.gpt-6-luna"])
	}
	if len(lines) != len(ConfigKeys())-1+len(config.User.Prices) {
		t.Errorf("printed %d lines; want one per key and one per price:\n%s", len(lines), out.String())
	}
}

func TestWriteMergedConfigPrintsRuleSets(t *testing.T) {
	repo := t.TempDir()
	repoPath := filepath.Join(repo, ".reviewrules", "settings.toml")
	writeConfigFile(t, repoPath, `
[[rule_sets]]
name = "go"
url = "file:///rules/go"
ref = "v1"

[[rule_sets]]
name = "sql"
url = "file:///rules/sql"
ref = "main"
`)
	config, err := LoadReviewConfig(filepath.Join(t.TempDir(), "none.toml"), UserConfigCaseLocal, repo)
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	err = config.WriteMergedConfig(&out, map[string]string{"go": "0123456789abcdef0123456789abcdef01234567"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`rule_sets.go                     = {name = "go", url = "file:///rules/go", ref = "v1", commit = "0123456789abcdef0123456789abcdef01234567"} # ` + repoPath,
		`rule_sets.sql                    = {name = "sql", url = "file:///rules/sql", ref = "main", commit = "not fetched"} # ` + repoPath,
	} {
		if !strings.Contains(out.String(), want+"\n") {
			t.Errorf("output has no line %q:\n%s", want, out.String())
		}
	}
}

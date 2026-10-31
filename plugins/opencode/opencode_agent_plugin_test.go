// Package opencode_test runs the exec plugin reviewrules-agent-opencode through agentplugin.ExecAgentPlugin.
package opencode_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/agentplugin"
)

func opencodePlugin(t *testing.T) *agentplugin.ExecAgentPlugin {
	t.Helper()
	path, err := filepath.Abs(agentplugin.ExecPluginPrefix + "opencode")
	if err != nil {
		t.Fatal(err)
	}
	return agentplugin.NewExecAgentPlugin(path)
}

// stubFiles are the files the stub opencode writes on each `run`.
type stubFiles struct {
	args         string
	config       string
	systemPrompt string
}

// useStubOpencode puts testdata/opencode first on PATH.
func useStubOpencode(t *testing.T) stubFiles {
	t.Helper()
	stubDir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BG_HOME", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	dir := t.TempDir()
	files := stubFiles{
		args:         filepath.Join(dir, "args"),
		config:       filepath.Join(dir, "config.json"),
		systemPrompt: filepath.Join(dir, "system-prompt"),
	}
	t.Setenv("OPENCODE_STUB_ARGS_FILE", files.args)
	t.Setenv("OPENCODE_STUB_CONFIG_FILE", files.config)
	t.Setenv("OPENCODE_STUB_SYSTEM_PROMPT_FILE", files.systemPrompt)
	return files
}

func readStubFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test owns the path.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// argValue returns the arg after flag, and "" when flag is absent.
func argValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

// reviewAgentConfig is the part of OPENCODE_CONFIG_CONTENT the plugin MUST set.
type reviewAgentConfig struct {
	Agent map[string]struct {
		Mode       string            `json:"mode"`
		Steps      int               `json:"steps"`
		Variant    string            `json:"variant"`
		Permission map[string]string `json:"permission"`
	} `json:"agent"`
}

func stubRequest(t *testing.T) agentplugin.PluginRequest {
	t.Helper()
	return agentplugin.PluginRequest{
		ProtocolVersion: agentplugin.PluginProtocolVersion,
		Prompt:          "-check the rule",
		SystemPrompt:    "you review code {env:HOME} {file:/etc/hosts}\n",
		Model:           "provider/model-1",
		ReasoningEffort: "high",
		MaxSteps:        8,
		RepoRoot:        t.TempDir(),
		PromptCacheKey:  "cache-key",
	}
}

func TestOpencodeAgentPluginOperationArgs(t *testing.T) {
	files := useStubOpencode(t)
	plugin := opencodePlugin(t)

	cases := []struct {
		operation agentplugin.PluginOperation
		sessionID string
		// wantSession is the --session value; "" accepts a new id with the "ses_" prefix.
		wantSession   string
		wantFork      bool
		wantSessionID string
	}{
		{operation: agentplugin.OperationStartParent},
		{
			operation:     agentplugin.OperationFork,
			sessionID:     "ses_parent-1",
			wantSession:   "ses_parent-1",
			wantFork:      true,
			wantSessionID: "ses_fork-1",
		},
		{
			operation:     agentplugin.OperationAppend,
			sessionID:     "ses_fork-1",
			wantSession:   "ses_fork-1",
			wantSessionID: "ses_fork-1",
		},
	}
	for _, tc := range cases {
		t.Run(string(tc.operation), func(t *testing.T) {
			request := stubRequest(t)
			request.SessionID = tc.sessionID

			reply, err := plugin.RunOperation(t.Context(), tc.operation, request)
			if err != nil {
				t.Fatal(err)
			}

			args := strings.Split(strings.TrimSuffix(readStubFile(t, files.args), "\n"), "\n")
			wantFlags := map[string]string{
				"--format": "json",
				"--agent":  "reviewrules",
				"--model":  "provider/model-1#high",
			}
			for flag, want := range wantFlags {
				if got := argValue(args, flag); got != want {
					t.Errorf("%s = %q; want %q in args %q", flag, got, want, args)
				}
			}
			if args[0] != "run" || !slices.Contains(args, "--standalone") {
				t.Errorf("args %q; want `run --standalone`", args)
			}
			if slices.Contains(args, "--fork") != tc.wantFork {
				t.Errorf("args %q: --fork present = %t; want %t", args, !tc.wantFork, tc.wantFork)
			}
			session := argValue(args, "--session")
			if tc.wantSession != "" && session != tc.wantSession {
				t.Errorf("--session = %q; want %q", session, tc.wantSession)
			}
			if tc.wantSession == "" && !strings.HasPrefix(session, "ses_") {
				t.Errorf("--session = %q; want a new id with the ses_ prefix", session)
			}
			if args[len(args)-2] != "--" || args[len(args)-1] != request.Prompt {
				t.Errorf("args end with %q; want -- and the prompt", args[len(args)-2:])
			}

			var config reviewAgentConfig
			err = json.Unmarshal([]byte(readStubFile(t, files.config)), &config)
			if err != nil {
				t.Fatal(err)
			}
			agent := config.Agent["reviewrules"]
			wantPermission := map[string]string{"*": "deny", "read": "allow", "grep": "allow", "glob": "allow"}
			if !maps.Equal(agent.Permission, wantPermission) {
				t.Errorf("permission = %v; want read-only %v", agent.Permission, wantPermission)
			}
			if agent.Mode != "primary" || agent.Steps != request.MaxSteps || agent.Variant != "" {
				t.Errorf("agent = %+v; want mode primary, steps %d, no variant", agent, request.MaxSteps)
			}
			if got := readStubFile(t, files.systemPrompt); got != request.SystemPrompt {
				t.Errorf("system prompt = %q; want %q unchanged", got, request.SystemPrompt)
			}

			wantSessionID := tc.wantSessionID
			if wantSessionID == "" {
				wantSessionID = session
			}
			if reply.SessionID != wantSessionID {
				t.Errorf("session_id = %q; want %q", reply.SessionID, wantSessionID)
			}
			if reply.Text != "stub reply" {
				t.Errorf("text = %q; want the text of the last assistant message", reply.Text)
			}
			wantUsage := agentplugin.PluginUsage{
				InputTokens:       30,
				CachedInputTokens: 300,
				OutputTokens:      5,
				CacheWriteTokens:  5,
				ReasoningTokens:   1,
			}
			if reply.Usage != wantUsage {
				t.Errorf("usage = %+v; want the sum of the messages after the last prompt %+v", reply.Usage, wantUsage)
			}
		})
	}
}

func TestOpencodeAgentPluginEffortWithoutModel(t *testing.T) {
	files := useStubOpencode(t)
	request := stubRequest(t)
	request.Model = ""
	request.MaxSteps = 0

	_, err := opencodePlugin(t).RunOperation(t.Context(), agentplugin.OperationStartParent, request)
	if err != nil {
		t.Fatal(err)
	}

	args := strings.Split(readStubFile(t, files.args), "\n")
	if slices.Contains(args, "--model") {
		t.Errorf("args %q have --model; want opencode's default model", args)
	}
	var config reviewAgentConfig
	err = json.Unmarshal([]byte(readStubFile(t, files.config)), &config)
	if err != nil {
		t.Fatal(err)
	}
	agent := config.Agent["reviewrules"]
	if agent.Variant != request.ReasoningEffort || agent.Steps != 0 {
		t.Errorf("agent = %+v; want variant %q and no steps", agent, request.ReasoningEffort)
	}
}

func TestOpencodeAgentPluginFails(t *testing.T) {
	useStubOpencode(t)
	plugin := opencodePlugin(t)

	cases := []struct {
		name      string
		stubError string
		version   int
		operation agentplugin.PluginOperation
		wantError string
	}{
		{"model-error", "1", agentplugin.PluginProtocolVersion, agentplugin.OperationStartParent, "stub failure"},
		{"unknown-protocol-version", "", 2, agentplugin.OperationStartParent, "protocol_version 2 is not supported"},
		{"fork-without-session", "", agentplugin.PluginProtocolVersion, agentplugin.OperationFork, "fork request has no session_id"},
		{"append-without-session", "", agentplugin.PluginProtocolVersion, agentplugin.OperationAppend, "append request has no session_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPENCODE_STUB_ERROR", tc.stubError)
			request := stubRequest(t)
			request.ProtocolVersion = tc.version

			_, err := plugin.RunOperation(t.Context(), tc.operation, request)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v; want one with %q", err, tc.wantError)
			}
		})
	}
}

// TestOpencodeAgentPluginLive runs the real opencode CLI. REVIEWRULES_OPENCODE_MODEL picks the model;
// empty uses opencode's default.
func TestOpencodeAgentPluginLive(t *testing.T) {
	if os.Getenv("REVIEWRULES_OPENCODE_LIVE") != "1" {
		t.Skip("set REVIEWRULES_OPENCODE_LIVE=1 to run the real opencode CLI")
	}
	t.Setenv("BG_HOME", t.TempDir())
	plugin := opencodePlugin(t)
	repoRoot := t.TempDir()
	request := agentplugin.PluginRequest{
		ProtocolVersion: agentplugin.PluginProtocolVersion,
		SystemPrompt:    "Answer in as few words as possible.",
		Model:           os.Getenv("REVIEWRULES_OPENCODE_MODEL"),
		MaxSteps:        4,
		RepoRoot:        repoRoot,
		PromptCacheKey:  "live",
	}

	request.Prompt = "The code word is kiwi. Try to create the file kiwi.txt, then reply with OK only."
	parent, err := plugin.RunOperation(t.Context(), agentplugin.OperationStartParent, request)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Usage.InputTokens+parent.Usage.CachedInputTokens == 0 {
		t.Errorf("start-parent usage = %+v; want input tokens", parent.Usage)
	}
	if _, err := os.Stat(filepath.Join(repoRoot, "kiwi.txt")); err == nil {
		t.Errorf("the model wrote kiwi.txt; want a read-only agent")
	}

	request.Prompt = "What is the code word? Reply with the word only."
	request.SessionID = parent.SessionID
	fork, err := plugin.RunOperation(t.Context(), agentplugin.OperationFork, request)
	if err != nil {
		t.Fatal(err)
	}
	if fork.SessionID == parent.SessionID || !strings.Contains(strings.ToLower(fork.Text), "kiwi") {
		t.Fatalf("fork reply = %+v; want a new session that knows the parent history", fork)
	}

	request.Prompt = "Say the code word again."
	request.SessionID = fork.SessionID
	again, err := plugin.RunOperation(t.Context(), agentplugin.OperationAppend, request)
	if err != nil {
		t.Fatal(err)
	}
	if again.SessionID != fork.SessionID || !strings.Contains(strings.ToLower(again.Text), "kiwi") {
		t.Fatalf("append reply = %+v; want the same fork with the code word", again)
	}
}

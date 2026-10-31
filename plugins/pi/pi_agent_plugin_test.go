// Package pi_test runs the exec plugin reviewrules-agent-pi through agentplugin.ExecAgentPlugin.
package pi_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/agentplugin"
)

const readOnlyTools = "read,grep,find,ls"

func piPlugin(t *testing.T) *agentplugin.ExecAgentPlugin {
	t.Helper()
	path, err := filepath.Abs(agentplugin.ExecPluginPrefix + "pi")
	if err != nil {
		t.Fatal(err)
	}
	return agentplugin.NewExecAgentPlugin(path)
}

// useStubPi puts testdata/pi first on PATH and returns the file the stub writes its args to.
func useStubPi(t *testing.T) string {
	t.Helper()
	stubDir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BG_HOME", t.TempDir())
	argsFile := filepath.Join(t.TempDir(), "pi-args")
	t.Setenv("PI_STUB_ARGS_FILE", argsFile)
	return argsFile
}

func readStubArgs(t *testing.T, argsFile string) []string {
	t.Helper()
	data, err := os.ReadFile(argsFile) // #nosec G304 -- the test owns the path.
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// flagValue returns the arg after flag, and "" when flag is absent.
func flagValue(args []string, flag string) string {
	i := slices.Index(args, flag)
	if i < 0 || i+1 >= len(args) {
		return ""
	}
	return args[i+1]
}

func stubRequest(t *testing.T) agentplugin.PluginRequest {
	t.Helper()
	return agentplugin.PluginRequest{
		ProtocolVersion: agentplugin.PluginProtocolVersion,
		Prompt:          "check the rule",
		SystemPrompt:    "you review code",
		Model:           "provider/model-1",
		ReasoningEffort: "high",
		RepoRoot:        t.TempDir(),
		PromptCacheKey:  "cache-key",
	}
}

func TestPiAgentPluginOperationArgs(t *testing.T) {
	argsFile := useStubPi(t)
	sessionDir := t.TempDir()
	t.Setenv("PI_SESSION_DIR", sessionDir)
	plugin := piPlugin(t)

	cases := []struct {
		operation agentplugin.PluginOperation
		sessionID string
		// sessionFlag names the arg whose value MUST be the reply session_id.
		sessionFlag string
		wantFlags   map[string]string
		absentFlags []string
	}{
		{
			operation:   agentplugin.OperationStartParent,
			sessionFlag: "--session-id",
			absentFlags: []string{"--fork", "--session"},
		},
		{
			operation:   agentplugin.OperationFork,
			sessionID:   "parent-1",
			sessionFlag: "--session-id",
			wantFlags:   map[string]string{"--fork": "parent-1"},
			absentFlags: []string{"--session"},
		},
		{
			operation:   agentplugin.OperationAppend,
			sessionID:   "fork-1",
			sessionFlag: "--session",
			wantFlags:   map[string]string{"--session": "fork-1"},
			absentFlags: []string{"--fork", "--session-id"},
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

			args := readStubArgs(t, argsFile)
			commonFlags := map[string]string{
				"--mode":          "json",
				"--tools":         readOnlyTools,
				"--system-prompt": request.SystemPrompt,
				"--model":         request.Model,
				"--thinking":      request.ReasoningEffort,
				"--session-dir":   sessionDir,
			}
			for _, flags := range []map[string]string{commonFlags, tc.wantFlags} {
				for flag, want := range flags {
					got := flagValue(args, flag)
					if got != want {
						t.Errorf("%s = %q; want %q in args %q", flag, got, want, args)
					}
				}
			}
			for _, flag := range tc.absentFlags {
				if slices.Contains(args, flag) {
					t.Errorf("args %q have %s; want it absent", args, flag)
				}
			}
			if args[len(args)-1] != request.Prompt {
				t.Errorf("last arg = %q; want the prompt", args[len(args)-1])
			}

			sessionID := flagValue(args, tc.sessionFlag)
			if sessionID == "" || reply.SessionID != sessionID {
				t.Errorf("session_id = %q; want the %s value %q", reply.SessionID, tc.sessionFlag, sessionID)
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
				t.Errorf("usage = %+v; want the sum of both assistant messages %+v", reply.Usage, wantUsage)
			}
		})
	}
}

func TestPiAgentPluginFails(t *testing.T) {
	useStubPi(t)
	t.Setenv("PI_SESSION_DIR", t.TempDir())
	plugin := piPlugin(t)

	cases := []struct {
		name       string
		stopReason string
		version    int
		wantError  string
	}{
		{"model-error", "error", agentplugin.PluginProtocolVersion, "stub failure"},
		{"unknown-protocol-version", "stop", 2, "protocol_version 2 is not supported"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PI_STUB_STOP_REASON", tc.stopReason)
			request := stubRequest(t)
			request.ProtocolVersion = tc.version

			_, err := plugin.RunOperation(t.Context(), agentplugin.OperationStartParent, request)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("error = %v; want one with %q", err, tc.wantError)
			}
		})
	}
}

// TestPiAgentPluginLive runs the real pi CLI. REVIEWRULES_PI_MODEL picks the model; empty uses pi's default.
func TestPiAgentPluginLive(t *testing.T) {
	if os.Getenv("REVIEWRULES_PI_LIVE") != "1" {
		t.Skip("set REVIEWRULES_PI_LIVE=1 to run the real pi CLI")
	}
	t.Setenv("BG_HOME", t.TempDir())
	t.Setenv("PI_SESSION_DIR", t.TempDir())
	plugin := piPlugin(t)
	request := agentplugin.PluginRequest{
		ProtocolVersion: agentplugin.PluginProtocolVersion,
		SystemPrompt:    "Answer in as few words as possible.",
		Model:           os.Getenv("REVIEWRULES_PI_MODEL"),
		RepoRoot:        t.TempDir(),
		PromptCacheKey:  "live",
	}

	request.Prompt = "The code word is kiwi. Reply with OK only."
	parent, err := plugin.RunOperation(t.Context(), agentplugin.OperationStartParent, request)
	if err != nil {
		t.Fatal(err)
	}
	if parent.Usage.InputTokens+parent.Usage.CachedInputTokens == 0 {
		t.Errorf("start-parent usage = %+v; want input tokens", parent.Usage)
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

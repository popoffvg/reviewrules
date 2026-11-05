package agentplugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/review"
)

func TestExecAgentPluginRunsEveryOperation(t *testing.T) {
	t.Setenv("BG_HOME", t.TempDir())
	plugin, err := FindExecAgentPlugin("testdata", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	agent := NewPluginForkAgent(plugin, t.TempDir(), "cache-key", "")
	settings := review.ForkSettings{Model: "small", MaxSteps: 2}

	parent, err := agent.StartParentSession(t.Context(), "parent prompt", settings)
	usage := parent.Usage
	if err != nil {
		t.Fatal(err)
	}
	if usage.InputTokens != 10 || usage.CacheReadTokens != 90 || usage.OutputTokens != 5 {
		t.Fatalf("start-parent usage = %+v; want input 10, cached 90, output 5", usage)
	}

	fork, err := agent.ForkParentSession(t.Context(), "rule prompt", settings)
	if err != nil {
		t.Fatal(err)
	}
	if fork.ForkID != "fork-1" || fork.Text != "fork done" {
		t.Fatalf("fork reply = %+v; want fork-1 and the fork text", fork)
	}

	again, err := agent.AppendToFork(t.Context(), fork.ForkID, "json only", settings)
	if err != nil {
		t.Fatal(err)
	}
	if again.ForkID != "fork-1" || again.Text != "append done" {
		t.Fatalf("append reply = %+v; want fork-1 and the append text", again)
	}
}

func TestExecAgentPluginNonZeroExitIsError(t *testing.T) {
	t.Setenv("BG_HOME", t.TempDir())
	plugin, err := FindExecAgentPlugin("testdata", "fixed")
	if err != nil {
		t.Fatal(err)
	}
	request := PluginRequest{ProtocolVersion: PluginProtocolVersion, RepoRoot: t.TempDir()}

	_, err = plugin.RunOperation(t.Context(), "review-all", request)
	if err == nil || !strings.Contains(err.Error(), "operation review-all is unknown") {
		t.Fatalf("error = %v; want the plugin's stderr", err)
	}
}

func TestFindExecAgentPluginRejects(t *testing.T) {
	pluginDir := t.TempDir()
	err := os.WriteFile(filepath.Join(pluginDir, ExecPluginPrefix+"plain"), []byte("#!/bin/sh\n"), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		agent string
	}{
		{"missing-file", "absent"},
		{"not-executable", "plain"},
		{"path-in-name", "../plain"},
		{"empty-name", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FindExecAgentPlugin(pluginDir, tc.agent)
			if err == nil {
				t.Fatalf("FindExecAgentPlugin(%q) found a plugin; want an error", tc.agent)
			}
		})
	}
}

func TestForkHypothesisLedgerSetsTheProtocolField(t *testing.T) {
	plugin := &capturePlugin{}
	agent := NewPluginForkAgent(plugin, t.TempDir(), "cache-key", "system")
	if _, err := agent.StartParentSession(t.Context(), "parent", review.ForkSettings{}); err != nil {
		t.Fatal(err)
	}
	_, err := agent.ForkHypothesisLedger(t.Context(), "answer the ledger", review.ForkSettings{Model: "composer-2.5"}, []review.Hypothesis{
		{ID: "H1", Hunt: "Boundaries", At: "a.go:1", Question: "Does the last second overflow?", Look: "snapid.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plugin.request.Hypotheses) != 1 || plugin.request.Hypotheses[0].ID != "H1" || plugin.request.Hypotheses[0].At != "a.go:1" {
		t.Fatalf("prompt %q hypotheses = %+v; want H1", plugin.request.Prompt, plugin.request.Hypotheses)
	}
	if _, err := agent.ForkParentSession(t.Context(), "one rule", review.ForkSettings{}); err != nil {
		t.Fatal(err)
	}
	if len(plugin.request.Hypotheses) != 0 {
		t.Fatalf("rule fork hypotheses = %+v; want none", plugin.request.Hypotheses)
	}
}

type capturePlugin struct {
	request PluginRequest
}

func (p *capturePlugin) RunOperation(_ context.Context, operation PluginOperation, request PluginRequest) (PluginReply, error) {
	p.request = request
	sessionID := request.SessionID
	if operation == OperationStartParent {
		sessionID = "parent"
	}
	if operation == OperationFork {
		sessionID = "fork-1"
	}
	return PluginReply{SessionID: sessionID, Text: "ok"}, nil
}

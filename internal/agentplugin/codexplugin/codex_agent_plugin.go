// Package codexplugin is the built-in codex agent plugin: it runs the local codex binary with its own login.
package codexplugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/popoffvg/background/launch"

	"github.com/popoffvg/reviewrules/internal/agentplugin"
)

var _ agentplugin.AgentPlugin = (*CodexAgentPlugin)(nil)

// CodexAgentPlugin runs each operation as one `codex exec` call. A fork is `codex exec fork <parent thread>`.
// codex keeps its own base instructions (a session override of base_instructions is ignored),
// so the system prompt goes in developer_instructions.
type CodexAgentPlugin struct {
	binary      string
	mu          sync.Mutex
	threadUsage map[string]codexCumulativeUsage
}

// NewCodexAgentPlugin builds the plugin; binary is "codex" or a path to it.
func NewCodexAgentPlugin(binary string) *CodexAgentPlugin {
	return &CodexAgentPlugin{binary: binary, threadUsage: make(map[string]codexCumulativeUsage)}
}

type codexCumulativeUsage struct {
	uncachedInput int64
	cachedInput   int64
	output        int64
	reasoning     int64
}

func codexCumulativeFromEvent(event codexExecEvent) codexCumulativeUsage {
	return codexCumulativeUsage{
		uncachedInput: event.Usage.InputTokens - event.Usage.CachedInputTokens,
		cachedInput:   event.Usage.CachedInputTokens,
		output:        event.Usage.OutputTokens,
		reasoning:     event.Usage.ReasoningOutputTokens,
	}
}

func (prev codexCumulativeUsage) delta(next codexCumulativeUsage) agentplugin.PluginUsage {
	return agentplugin.PluginUsage{
		InputTokens:       next.uncachedInput - prev.uncachedInput,
		CachedInputTokens: next.cachedInput - prev.cachedInput,
		OutputTokens:      next.output - prev.output,
		ReasoningTokens:   next.reasoning - prev.reasoning,
	}
}

func addPluginUsage(total, more agentplugin.PluginUsage) agentplugin.PluginUsage {
	total.InputTokens += more.InputTokens
	total.CachedInputTokens += more.CachedInputTokens
	total.OutputTokens += more.OutputTokens
	total.ReasoningTokens += more.ReasoningTokens
	return total
}

type codexExecEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"item"`
	Usage struct {
		InputTokens           int64 `json:"input_tokens"`
		CachedInputTokens     int64 `json:"cached_input_tokens"`
		OutputTokens          int64 `json:"output_tokens"`
		ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	} `json:"usage"`
}

// RunOperation maps the operation to the codex exec subcommand.
func (p *CodexAgentPlugin) RunOperation(
	ctx context.Context,
	operation agentplugin.PluginOperation,
	request agentplugin.PluginRequest,
) (agentplugin.PluginReply, error) {
	var subcommand []string
	switch operation {
	case agentplugin.OperationStartParent:
	case agentplugin.OperationFork:
		subcommand = []string{"fork", request.SessionID}
	case agentplugin.OperationAppend:
		subcommand = []string{"resume", request.SessionID}
	default:
		return agentplugin.PluginReply{}, fmt.Errorf("codex plugin operation %q is unknown", operation)
	}
	return p.runCodex(ctx, subcommand, request)
}

// runCodex puts every option before the fork or resume subcommand: the subcommands reject -s and -m.
// codex has no step limit, so request.MaxSteps is not used.
func (p *CodexAgentPlugin) runCodex(
	ctx context.Context,
	subcommand []string,
	request agentplugin.PluginRequest,
) (agentplugin.PluginReply, error) {
	// A JSON string is a valid TOML basic string, which is what -c parses.
	developerInstructions, _ := json.Marshal(request.SystemPrompt)
	args := []string{
		"exec", "--json", "--skip-git-repo-check", "-s", "read-only",
		"-c", "developer_instructions=" + string(developerInstructions),
		// 0 skips AGENTS.md: the review reads the repo's rules from the rule folders, not the agent docs.
		"-c", "project_doc_max_bytes=0",
	}
	if request.Model != "" {
		args = append(args, "-m", request.Model)
	}
	if request.ReasoningEffort != "" {
		args = append(args, "-c", "model_reasoning_effort="+request.ReasoningEffort)
	}
	args = append(args, subcommand...)
	args = append(args, "--", request.Prompt)

	var stdout, stderr bytes.Buffer
	run, err := launch.Run(ctx, p.binary, args, launch.Options{
		Dir:      request.RepoRoot,
		Stdout:   &stdout,
		Stderr:   &stderr,
		SourceID: "reviewrules",
	})
	if err != nil {
		return agentplugin.PluginReply{}, fmt.Errorf("codex run failed: %w", err)
	}
	if run.ExitCode != 0 {
		return agentplugin.PluginReply{}, fmt.Errorf(
			"codex run exited with %d: %s", run.ExitCode, strings.TrimSpace(stderr.String()),
		)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	baseline := p.threadUsage[request.SessionID]
	prev := baseline
	var reply agentplugin.PluginReply
	scanner := bufio.NewScanner(&stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var event codexExecEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch {
		case event.Type == "thread.started":
			reply.SessionID = event.ThreadID
		case event.Type == "item.completed" && event.Item.Type == "agent_message":
			reply.Text = event.Item.Text
		case event.Type == "turn.completed":
			next := codexCumulativeFromEvent(event)
			reply.Usage = addPluginUsage(reply.Usage, prev.delta(next))
			prev = next
		}
	}
	if reply.SessionID == "" {
		return reply, fmt.Errorf("codex output has no thread.started event: %s", strings.TrimSpace(stderr.String()))
	}
	p.threadUsage[reply.SessionID] = prev
	return reply, nil
}

// Package claudeplugin is the built-in claude agent plugin: it runs the local claude binary with its own login.
package claudeplugin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/popoffvg/background/launch"

	"github.com/popoffvg/reviewrules/internal/agentplugin"
)

var _ agentplugin.AgentPlugin = (*ClaudeAgentPlugin)(nil)

// ClaudeAgentPlugin runs each operation as one `claude -p` call.
// A fork is `claude -p --resume <parent> --fork-session`, which reads the parent from the cache.
type ClaudeAgentPlugin struct {
	binary string
}

// NewClaudeAgentPlugin builds the plugin; binary is "claude" or a path to it.
func NewClaudeAgentPlugin(binary string) *ClaudeAgentPlugin {
	return &ClaudeAgentPlugin{binary: binary}
}

type claudePrintResult struct {
	SessionID string `json:"session_id"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	Subtype   string `json:"subtype"`
	Usage     struct {
		InputTokens              int64 `json:"input_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
	} `json:"usage"`
}

// RunOperation maps the operation to the claude session flags.
func (p *ClaudeAgentPlugin) RunOperation(
	ctx context.Context,
	operation agentplugin.PluginOperation,
	request agentplugin.PluginRequest,
) (agentplugin.PluginReply, error) {
	var sessionArgs []string
	switch operation {
	case agentplugin.OperationStartParent:
		sessionArgs = []string{"--session-id", uuid.NewString()}
	case agentplugin.OperationFork:
		sessionArgs = []string{"--resume", request.SessionID, "--fork-session"}
	case agentplugin.OperationAppend:
		sessionArgs = []string{"--resume", request.SessionID}
	default:
		return agentplugin.PluginReply{}, fmt.Errorf("claude plugin operation %q is unknown", operation)
	}
	return p.runClaude(ctx, sessionArgs, request)
}

// runClaude starts claude without the user's settings, hooks, MCP servers, and CLAUDE.md
// system prompt: with them a fork's prefix was 256K tokens, without them 3K.
func (p *ClaudeAgentPlugin) runClaude(
	ctx context.Context,
	sessionArgs []string,
	request agentplugin.PluginRequest,
) (agentplugin.PluginReply, error) {
	args := []string{
		"-p",
		"--output-format", "json",
		"--tools", "Read,Grep,Glob",
		"--allowedTools", "Read,Grep,Glob",
		"--strict-mcp-config",
		"--setting-sources", "",
		"--system-prompt", request.SystemPrompt,
	}
	args = append(args, sessionArgs...)
	if request.Model != "" {
		args = append(args, "--model", request.Model)
	}
	if request.ReasoningEffort != "" {
		args = append(args, "--effort", request.ReasoningEffort)
	}
	if request.MaxSteps > 0 {
		args = append(args, "--max-turns", strconv.Itoa(request.MaxSteps))
	}
	args = append(args, "--", request.Prompt)

	var stdout, stderr bytes.Buffer
	run, err := launch.Run(ctx, p.binary, args, launch.Options{
		Dir:      request.RepoRoot,
		Stdout:   &stdout,
		Stderr:   &stderr,
		SourceID: "reviewrules",
	})
	if err != nil {
		return agentplugin.PluginReply{}, fmt.Errorf("claude run failed: %w", err)
	}
	if run.ExitCode != 0 {
		return agentplugin.PluginReply{}, fmt.Errorf(
			"claude run exited with %d: %s", run.ExitCode, strings.TrimSpace(stderr.String()),
		)
	}
	var result claudePrintResult
	err = json.Unmarshal(stdout.Bytes(), &result)
	if err != nil {
		return agentplugin.PluginReply{}, fmt.Errorf("claude output is not JSON: %w", err)
	}
	reply := agentplugin.PluginReply{
		SessionID: result.SessionID,
		Text:      result.Result,
		Usage: agentplugin.PluginUsage{
			InputTokens:       result.Usage.InputTokens,
			CachedInputTokens: result.Usage.CacheReadInputTokens,
			CacheWriteTokens:  result.Usage.CacheCreationInputTokens,
			OutputTokens:      result.Usage.OutputTokens,
		},
	}
	// error_max_turns still carries usage and a session to append to, so it is not a failure here.
	if result.IsError && result.Subtype != "error_max_turns" {
		return reply, fmt.Errorf("claude run returned %s: %s", result.Subtype, result.Result)
	}
	return reply, nil
}

// Package agentplugin defines the agent plugin protocol. reviewrules runs `reviewrules-agent-<name> <operation>`
// with operation start-parent, fork, or append. It writes one PluginRequest as JSON to stdin and reads
// one PluginReply as JSON from stdout. A non-zero exit is an error, and stderr is its message.
// The built-in codex and claude plugins implement AgentPlugin in-process.
package agentplugin

import "context"

// PluginProtocolVersion is the protocol_version of every request. A plugin MUST reject a version it does not know.
const PluginProtocolVersion = 1

// PluginOperation is the first argument of a plugin run; one value per ReviewForkAgent method.
type PluginOperation string

// PluginOperation values.
const (
	// OperationStartParent runs the parent prompt in a new session.
	OperationStartParent PluginOperation = "start-parent"
	// OperationFork runs the prompt as a new turn in a copy of the session SessionID names.
	OperationFork PluginOperation = "fork"
	// OperationAppend adds one turn to the end of the session SessionID names; it makes no new session.
	OperationAppend PluginOperation = "append"
)

// PluginRequest is the JSON on the plugin's stdin. An empty Model or ReasoningEffort uses the plugin's
// default, and MaxSteps 0 means no step limit.
type PluginRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	Prompt          string `json:"prompt"`
	SystemPrompt    string `json:"system_prompt"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	MaxSteps        int    `json:"max_steps"`
	RepoRoot        string `json:"repo_root"`
	PromptCacheKey  string `json:"prompt_cache_key"`
	// SessionID is empty for start-parent. For fork it is the session to copy: the parent or a fork.
	// For append it is the session to add the turn to.
	SessionID string `json:"session_id,omitempty"`
}

// PluginReply is the JSON on the plugin's stdout. SessionID names the session this run wrote to:
// the parent session for start-parent, the new fork for fork, the same session for append.
type PluginReply struct {
	SessionID string      `json:"session_id"`
	Text      string      `json:"text"`
	Usage     PluginUsage `json:"usage"`
}

// PluginUsage counts the tokens of one run. InputTokens leaves out CachedInputTokens.
// CacheWriteTokens and ReasoningTokens are optional.
type PluginUsage struct {
	InputTokens       int64 `json:"input_tokens"`
	CachedInputTokens int64 `json:"cached_input_tokens"`
	OutputTokens      int64 `json:"output_tokens"`
	CacheWriteTokens  int64 `json:"cache_write_tokens,omitempty"`
	ReasoningTokens   int64 `json:"reasoning_tokens,omitempty"`
}

// AgentPlugin runs one protocol operation. A built-in plugin runs it in-process; ExecAgentPlugin
// runs an external executable. A reply that comes with an error can still carry usage.
type AgentPlugin interface {
	RunOperation(ctx context.Context, operation PluginOperation, request PluginRequest) (PluginReply, error)
}

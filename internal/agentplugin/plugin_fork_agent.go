package agentplugin

import (
	"context"

	"charm.land/fantasy"

	"github.com/popoffvg/reviewrules/internal/prompts"
	"github.com/popoffvg/reviewrules/internal/review"
)

var _ review.ReviewForkAgent = (*PluginForkAgent)(nil)

// PluginForkAgent runs a review through an AgentPlugin. It keeps the parent session id that
// start-parent returns and sends it with every fork.
type PluginForkAgent struct {
	plugin          AgentPlugin
	repoRoot        string
	promptCacheKey  string
	parentSessionID string
}

// NewPluginForkAgent builds the agent for one run.
func NewPluginForkAgent(plugin AgentPlugin, repoRoot string, promptCacheKey string) *PluginForkAgent {
	return &PluginForkAgent{plugin: plugin, repoRoot: repoRoot, promptCacheKey: promptCacheKey}
}

// StartParentSession runs start-parent.
func (a *PluginForkAgent) StartParentSession(
	ctx context.Context,
	prompt string,
	settings review.ForkSettings,
) (fantasy.Usage, error) {
	reply, err := a.plugin.RunOperation(ctx, OperationStartParent, a.pluginRequest(prompt, settings))
	a.parentSessionID = reply.SessionID
	return reply.Usage.fantasyUsage(), err
}

// ForkParentSession runs fork with the parent session id.
func (a *PluginForkAgent) ForkParentSession(
	ctx context.Context,
	prompt string,
	settings review.ForkSettings,
) (review.ForkReply, error) {
	request := a.pluginRequest(prompt, settings)
	request.SessionID = a.parentSessionID
	reply, err := a.plugin.RunOperation(ctx, OperationFork, request)
	return reply.forkReply(), err
}

// AppendToFork runs append with the fork id.
func (a *PluginForkAgent) AppendToFork(
	ctx context.Context,
	forkID string,
	prompt string,
	settings review.ForkSettings,
) (review.ForkReply, error) {
	request := a.pluginRequest(prompt, settings)
	request.SessionID = forkID
	reply, err := a.plugin.RunOperation(ctx, OperationAppend, request)
	return reply.forkReply(), err
}

func (a *PluginForkAgent) pluginRequest(prompt string, settings review.ForkSettings) PluginRequest {
	return PluginRequest{
		ProtocolVersion: PluginProtocolVersion,
		Prompt:          prompt,
		SystemPrompt:    prompts.ReviewSystem,
		Model:           settings.Model,
		ReasoningEffort: settings.ReasoningEffort,
		MaxSteps:        settings.MaxSteps,
		RepoRoot:        a.repoRoot,
		PromptCacheKey:  a.promptCacheKey,
	}
}

func (r PluginReply) forkReply() review.ForkReply {
	return review.ForkReply{ForkID: r.SessionID, Text: r.Text, Usage: r.Usage.fantasyUsage()}
}

func (u PluginUsage) fantasyUsage() fantasy.Usage {
	return fantasy.Usage{
		InputTokens:         u.InputTokens,
		CacheReadTokens:     u.CachedInputTokens,
		OutputTokens:        u.OutputTokens,
		CacheCreationTokens: u.CacheWriteTokens,
		ReasoningTokens:     u.ReasoningTokens,
	}
}

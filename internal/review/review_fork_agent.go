package review

import (
	"cmp"
	"context"
	"fmt"

	"charm.land/fantasy"
)

// ReviewForkAgent runs one parent session that holds the diff and forks it once per rule and per
// hypothesis. A fork starts from the parent's full history, so the backend can serve that history
// from its prompt cache. Implementations: Fantasy over an OpenAI-compatible endpoint (LiteLLM),
// and agentplugin.PluginForkAgent for the built-in and exec agent plugins.
// An agent runs one parent session in its lifetime.
type ReviewForkAgent interface {
	// StartParentSession runs the parent prompt to the end. It MUST finish before the first fork.
	StartParentSession(ctx context.Context, prompt string, settings ForkSettings) (fantasy.Usage, error)
	// ForkParentSession runs prompt as a new turn after the parent history; forks may run in parallel.
	ForkParentSession(ctx context.Context, prompt string, settings ForkSettings) (ForkReply, error)
	// ForkHypothesisLedger is the one checker fork. hypotheses is the protocol field of that request,
	// and prompt repeats the same rows for a backend that only reads the prompt.
	ForkHypothesisLedger(ctx context.Context, prompt string, settings ForkSettings, hypotheses []Hypothesis) (ForkReply, error)
	// AppendToFork adds one turn to the end of a finished fork, for example to ask again for the JSON answer.
	AppendToFork(ctx context.Context, forkID string, prompt string, settings ForkSettings) (ForkReply, error)
}

// ReviewForkAgentFactory builds the agent for one run once the diff is known; promptCacheKey
// names that diff's cache, and backends that route their cache by a key MUST send it on every call.
type ReviewForkAgentFactory func(promptCacheKey string) ReviewForkAgent

// ForkSettings selects the model, the reasoning effort, and the tool-call budget of one call.
// An empty Model or ReasoningEffort uses the backend's default. In a thinking profile of the user
// config, an empty field keeps the value of the rule forks.
type ForkSettings struct {
	Model           string `json:"model,omitempty"            toml:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty" toml:"reasoning_effort,omitempty"`
	MaxSteps        int    `json:"fork_steps,omitempty"       toml:"fork_steps,omitzero"`
}

// withProfile gives the settings of a rule fork: each non-empty field of profile replaces the base value.
func (base ForkSettings) withProfile(profile ForkSettings) ForkSettings {
	return ForkSettings{
		Model:           cmp.Or(profile.Model, base.Model),
		ReasoningEffort: cmp.Or(profile.ReasoningEffort, base.ReasoningEffort),
		MaxSteps:        cmp.Or(profile.MaxSteps, base.MaxSteps),
	}
}

// ForkReply is the last text a fork wrote, its usage, and the id AppendToFork takes.
type ForkReply struct {
	ForkID string
	Text   string
	Usage  fantasy.Usage
}

// forkReplyJSON decodes a fork's JSON reply and asks the fork once more when the reply has none.
func forkReplyJSON(
	ctx context.Context,
	agent ReviewForkAgent,
	settings ForkSettings,
	fork ForkReply,
	requiredKey string,
	target any,
	retryPrompt string,
) (fantasy.Usage, string, error) {
	if decodeForkJSONReply(fork.Text, requiredKey, target) == nil {
		return fantasy.Usage{}, fork.Text, nil
	}
	retrySettings := settings
	retrySettings.MaxSteps = 1
	retry, err := agent.AppendToFork(ctx, fork.ForkID, retryPrompt, retrySettings)
	if err != nil {
		return retry.Usage, fork.Text, fmt.Errorf("fork JSON retry failed: %w", err)
	}
	return retry.Usage, retry.Text, decodeForkJSONReply(retry.Text, requiredKey, target)
}

// AddUsage sums two usage records.
func AddUsage(total fantasy.Usage, more fantasy.Usage) fantasy.Usage {
	return fantasy.Usage{
		InputTokens:         total.InputTokens + more.InputTokens,
		OutputTokens:        total.OutputTokens + more.OutputTokens,
		TotalTokens:         total.TotalTokens + more.TotalTokens,
		ReasoningTokens:     total.ReasoningTokens + more.ReasoningTokens,
		CacheCreationTokens: total.CacheCreationTokens + more.CacheCreationTokens,
		CacheReadTokens:     total.CacheReadTokens + more.CacheReadTokens,
	}
}

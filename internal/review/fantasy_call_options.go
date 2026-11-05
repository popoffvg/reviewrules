package review

import (
	"maps"
	"slices"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
)

// FantasyCallOptions gives the agent options of one model and effort for one provider family.
type FantasyCallOptions func(settings ForkSettings) []fantasy.AgentOption

// OpenAICompatCallOptions sends promptCacheKey on every call, so forks find the parent's prefix.
// Set liteLLMSession only for a LiteLLM proxy: OpenAI rejects the unknown litellm_session_id field.
func OpenAICompatCallOptions(promptCacheKey string, liteLLMSession bool) FantasyCallOptions {
	return func(settings ForkSettings) []fantasy.AgentOption {
		// LiteLLM's chatgpt provider drops prompt_cache_key and sends a new random session_id
		// header per call unless litellm_session_id is set; the ChatGPT backend caches per session.
		options := &openaicompat.ProviderOptions{
			ExtraBody: map[string]any{"prompt_cache_key": promptCacheKey},
		}
		if liteLLMSession {
			options.ExtraBody["litellm_session_id"] = promptCacheKey
		}
		if settings.ReasoningEffort != "" {
			effort := openai.ReasoningEffort(settings.ReasoningEffort)
			options.ReasoningEffort = &effort
		}
		return []fantasy.AgentOption{fantasy.WithProviderOptions(fantasy.ProviderOptions{openaicompat.Name: options})}
	}
}

// anthropicMaxOutputTokens replaces the provider default of 4096, which adaptive thinking can use up
// before the JSON answer.
const anthropicMaxOutputTokens = 16000

// AnthropicCallOptions sends the effort as output_config.effort; the Anthropic API caches by the
// breakpoint that withCacheBreakpoint sets, not by a key.
func AnthropicCallOptions() FantasyCallOptions {
	return func(settings ForkSettings) []fantasy.AgentOption {
		options := &anthropic.ProviderOptions{}
		if settings.ReasoningEffort != "" {
			effort := anthropic.Effort(settings.ReasoningEffort)
			options.Effort = &effort
		}
		return []fantasy.AgentOption{
			fantasy.WithMaxOutputTokens(anthropicMaxOutputTokens),
			fantasy.WithProviderOptions(fantasy.ProviderOptions{anthropic.Name: options}),
		}
	}
}

// withCacheBreakpoint marks the last message as the end of the cached prefix. The Anthropic API caches
// only up to such a mark, and its 20-block lookback finds the parent's entry from a fork; OpenAI-compatible
// providers ignore the mark.
func withCacheBreakpoint(messages []fantasy.Message) []fantasy.Message {
	marked := slices.Clone(messages)
	last := &marked[len(marked)-1]
	options := maps.Clone(last.ProviderOptions)
	if options == nil {
		options = fantasy.ProviderOptions{}
	}
	options[anthropic.Name] = &anthropic.ProviderCacheControlOptions{CacheControl: anthropic.CacheControl{Type: "ephemeral"}}
	last.ProviderOptions = options
	return marked
}

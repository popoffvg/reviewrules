package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"sync"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

var _ ReviewForkAgent = (*FantasyForkAgent)(nil)

// FantasyForkAgent runs the review through Fantasy on an OpenAI-compatible endpoint, a LiteLLM
// proxy in practice. A fork is a new call whose Messages are the parent's history.
type FantasyForkAgent struct {
	provider       fantasy.Provider
	repoRoot       string
	promptCacheKey string
	// liteLLMSession sends litellm_session_id, which only a LiteLLM proxy accepts.
	liteLLMSession bool
	systemPrompt   string
	lastStepAnswer string

	mutex   sync.Mutex
	agents  map[ForkSettings]fantasy.Agent
	history []fantasy.Message
	forks   map[string][]fantasy.Message
}

// NewFantasyForkAgent builds the agent. promptCacheKey names the provider cache for this run;
// every call of the run sends it, so forks find the parent's prefix. Set liteLLMSession only for
// a LiteLLM proxy: OpenAI rejects the unknown litellm_session_id field.
func NewFantasyForkAgent(
	provider fantasy.Provider,
	repoRoot string,
	promptCacheKey string,
	liteLLMSession bool,
	text prompts.Set,
) *FantasyForkAgent {
	if text.ReviewSystem == "" {
		text = prompts.Default()
	}
	return &FantasyForkAgent{
		provider:       provider,
		repoRoot:       repoRoot,
		promptCacheKey: promptCacheKey,
		liteLLMSession: liteLLMSession,
		systemPrompt:   text.ReviewSystem,
		lastStepAnswer: text.LastStepAnswer,
		agents:         map[ForkSettings]fantasy.Agent{},
		forks:          map[string][]fantasy.Message{},
	}
}

// ReviewPromptCacheKey names the cache for one diff, so a rerun on the same diff reuses it.
func ReviewPromptCacheKey(diff ReviewDiff) string {
	sum := sha256.Sum256([]byte(diff.Patch))
	return "reviewrules-" + hex.EncodeToString(sum[:8])
}

// StartParentSession runs the parent prompt and keeps its history for the forks.
func (a *FantasyForkAgent) StartParentSession(
	ctx context.Context,
	prompt string,
	settings ForkSettings,
) (fantasy.Usage, error) {
	agent, err := a.agentFor(ctx, settings)
	if err != nil {
		return fantasy.Usage{}, err
	}
	result, err := agent.Generate(ctx, fantasy.AgentCall{
		Prompt:   prompt,
		StopWhen: []fantasy.StopCondition{fantasy.StepCountIs(settings.MaxSteps)},
	})
	if err != nil {
		return fantasy.Usage{}, fmt.Errorf("parent session failed: %w", err)
	}
	history := []fantasy.Message{fantasy.NewUserMessage(prompt)}
	for _, step := range result.Steps {
		history = append(history, step.Messages...)
	}
	a.mutex.Lock()
	a.history = history
	a.mutex.Unlock()
	return result.TotalUsage, nil
}

// ForkParentSession runs prompt after the parent history.
func (a *FantasyForkAgent) ForkParentSession(
	ctx context.Context,
	prompt string,
	settings ForkSettings,
) (ForkReply, error) {
	a.mutex.Lock()
	history := slices.Clone(a.history)
	a.mutex.Unlock()
	return a.generateFork(ctx, "", history, prompt, settings)
}

// ForkHypothesisLedger runs one checker fork. The ledger is in prompt; fantasy has no protocol field.
func (a *FantasyForkAgent) ForkHypothesisLedger(
	ctx context.Context,
	prompt string,
	settings ForkSettings,
	_ []Hypothesis,
) (ForkReply, error) {
	return a.ForkParentSession(ctx, prompt, settings)
}

// AppendToFork runs prompt after a finished fork's history and keeps the fork id.
func (a *FantasyForkAgent) AppendToFork(
	ctx context.Context,
	forkID string,
	prompt string,
	settings ForkSettings,
) (ForkReply, error) {
	a.mutex.Lock()
	history, ok := a.forks[forkID]
	a.mutex.Unlock()
	if !ok {
		return ForkReply{}, fmt.Errorf("fantasy fork %q is unknown", forkID)
	}
	return a.generateFork(ctx, forkID, slices.Clone(history), prompt, settings)
}

// generateFork saves the history under forkID; an empty forkID gets a new id.
func (a *FantasyForkAgent) generateFork(
	ctx context.Context,
	forkID string,
	history []fantasy.Message,
	prompt string,
	settings ForkSettings,
) (ForkReply, error) {
	agent, err := a.agentFor(ctx, settings)
	if err != nil {
		return ForkReply{}, err
	}
	result, err := agent.Generate(ctx, fantasy.AgentCall{
		Messages:    history,
		Prompt:      prompt,
		StopWhen:    []fantasy.StopCondition{fantasy.StepCountIs(settings.MaxSteps)},
		PrepareStep: a.answerOnLastStep(settings.MaxSteps),
	})
	if err != nil {
		return ForkReply{}, err
	}
	forkHistory := append(slices.Clone(history), fantasy.NewUserMessage(prompt))
	for _, step := range result.Steps {
		forkHistory = append(forkHistory, step.Messages...)
	}
	a.mutex.Lock()
	if forkID == "" {
		forkID = strconv.Itoa(len(a.forks) + 1)
	}
	a.forks[forkID] = forkHistory
	a.mutex.Unlock()
	return ForkReply{ForkID: forkID, Text: result.Response.Content.Text(), Usage: result.TotalUsage}, nil
}

// agentFor builds one Fantasy agent per model and effort. Every agent keeps the same system
// prompt, tools, and cache key, so a fork on any of them still matches the parent's prefix.
func (a *FantasyForkAgent) agentFor(ctx context.Context, settings ForkSettings) (fantasy.Agent, error) {
	key := ForkSettings{Model: settings.Model, ReasoningEffort: settings.ReasoningEffort}
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if agent, ok := a.agents[key]; ok {
		return agent, nil
	}
	model, err := a.provider.LanguageModel(ctx, settings.Model)
	if err != nil {
		return nil, fmt.Errorf("fantasy model %q setup failed: %w", settings.Model, err)
	}
	// LiteLLM's chatgpt provider drops prompt_cache_key and sends a new random session_id
	// header per call unless litellm_session_id is set; the ChatGPT backend caches per session.
	options := &openaicompat.ProviderOptions{
		ExtraBody: map[string]any{"prompt_cache_key": a.promptCacheKey},
	}
	if a.liteLLMSession {
		options.ExtraBody["litellm_session_id"] = a.promptCacheKey
	}
	if settings.ReasoningEffort != "" {
		effort := openai.ReasoningEffort(settings.ReasoningEffort)
		options.ReasoningEffort = &effort
	}
	agent := fantasy.NewAgent(
		model,
		fantasy.WithSystemPrompt(a.systemPrompt),
		fantasy.WithTools(NewRepoReadTools(a.repoRoot)...),
		fantasy.WithProviderOptions(fantasy.ProviderOptions{openaicompat.Name: options}),
	)
	a.agents[key] = agent
	return agent, nil
}

// answerOnLastStep forbids tool calls on the last allowed step, so a fork that spent its
// steps on reading still ends with a text answer. ToolChoiceNone keeps the tool list in
// the request, so the cached prefix still matches.
func (a *FantasyForkAgent) answerOnLastStep(maxSteps int) fantasy.PrepareStepFunction {
	return func(
		ctx context.Context,
		options fantasy.PrepareStepFunctionOptions,
	) (context.Context, fantasy.PrepareStepResult, error) {
		if options.StepNumber < maxSteps-1 {
			return ctx, fantasy.PrepareStepResult{}, nil
		}
		none := fantasy.ToolChoiceNone
		messages := append(slices.Clone(options.Messages), fantasy.NewUserMessage(a.lastStepAnswer))
		return ctx, fantasy.PrepareStepResult{ToolChoice: &none, Messages: messages}, nil
	}
}

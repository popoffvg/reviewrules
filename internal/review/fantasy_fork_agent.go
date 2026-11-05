package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"

	"charm.land/fantasy"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

var _ ReviewForkAgent = (*FantasyForkAgent)(nil)

// FantasyForkAgent runs the review through Fantasy on an API backend: an OpenAI-compatible
// endpoint or the Anthropic API. A fork is a new call whose Messages are the parent's history.
type FantasyForkAgent struct {
	provider       fantasy.Provider
	repoRoot       string
	callOptions    FantasyCallOptions
	systemPrompt   string
	lastStepAnswer string
	sessionDir     string

	mutex   sync.Mutex
	agents  map[ForkSettings]fantasy.Agent
	history []fantasy.Message
	forks   map[string][]fantasy.Message
}

// NewFantasyForkAgent builds the agent. callOptions gives the provider options of every call of the run.
// A non-empty sessionDir gets parent.json and one fork-<id>.json per fork with their messages.
func NewFantasyForkAgent(
	provider fantasy.Provider,
	repoRoot string,
	callOptions FantasyCallOptions,
	text prompts.Set,
	sessionDir string,
) *FantasyForkAgent {
	if text.ReviewSystem == "" {
		text = prompts.Default()
	}
	return &FantasyForkAgent{
		provider:       provider,
		repoRoot:       repoRoot,
		callOptions:    callOptions,
		systemPrompt:   text.ReviewSystem,
		lastStepAnswer: text.LastStepAnswer,
		sessionDir:     sessionDir,
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
) (ForkReply, error) {
	agent, err := a.agentFor(ctx, settings)
	if err != nil {
		return ForkReply{}, err
	}
	result, err := agent.Generate(ctx, fantasy.AgentCall{
		Prompt:      prompt,
		StopWhen:    []fantasy.StopCondition{fantasy.StepCountIs(settings.MaxSteps)},
		PrepareStep: cacheEachStep,
	})
	if err != nil {
		return ForkReply{}, fmt.Errorf("parent session failed: %w", err)
	}
	history := []fantasy.Message{fantasy.NewUserMessage(prompt)}
	for _, step := range result.Steps {
		history = append(history, step.Messages...)
	}
	a.mutex.Lock()
	a.history = history
	a.mutex.Unlock()
	err = a.saveSession("parent", append([]fantasy.Message{fantasy.NewSystemMessage(a.systemPrompt)}, history...))
	if err != nil {
		return ForkReply{}, err
	}
	return ForkReply{Usage: result.TotalUsage, Requests: stepUsages(result.Steps)}, nil
}

// stepUsages lists the usage of each model request of one Generate call.
func stepUsages(steps []fantasy.StepResult) []fantasy.Usage {
	usages := make([]fantasy.Usage, 0, len(steps))
	for _, step := range steps {
		usages = append(usages, step.Usage)
	}
	return usages
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
	parentLength := len(a.history)
	a.mutex.Unlock()
	err = a.saveSession("fork-"+forkID, forkHistory[parentLength:])
	if err != nil {
		return ForkReply{}, err
	}
	return ForkReply{
		ForkID:   forkID,
		Text:     result.Response.Content.Text(),
		Usage:    result.TotalUsage,
		Requests: stepUsages(result.Steps),
	}, nil
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
	agentOptions := []fantasy.AgentOption{
		fantasy.WithSystemPrompt(a.systemPrompt),
		fantasy.WithTools(NewRepoReadTools(a.repoRoot)...),
	}
	agent := fantasy.NewAgent(model, append(agentOptions, a.callOptions(settings)...)...)
	a.agents[key] = agent
	return agent, nil
}

// cacheEachStep moves the cache breakpoint to the end of each step's request.
func cacheEachStep(
	ctx context.Context,
	options fantasy.PrepareStepFunctionOptions,
) (context.Context, fantasy.PrepareStepResult, error) {
	return ctx, fantasy.PrepareStepResult{Messages: withCacheBreakpoint(options.Messages)}, nil
}

// answerOnLastStep forbids tool calls on the last allowed step, so a fork that spent its
// steps on reading still ends with a text answer. ToolChoiceNone keeps the tool list in
// the request, so the cached prefix still matches. Every step gets the cache breakpoint.
func (a *FantasyForkAgent) answerOnLastStep(maxSteps int) fantasy.PrepareStepFunction {
	return func(
		ctx context.Context,
		options fantasy.PrepareStepFunctionOptions,
	) (context.Context, fantasy.PrepareStepResult, error) {
		if options.StepNumber < maxSteps-1 {
			return cacheEachStep(ctx, options)
		}
		none := fantasy.ToolChoiceNone
		messages := append(slices.Clone(options.Messages), fantasy.NewUserMessage(a.lastStepAnswer))
		return ctx, fantasy.PrepareStepResult{ToolChoice: &none, Messages: withCacheBreakpoint(messages)}, nil
	}
}

// saveSession writes messages to <name>.json in the session folder. A fork file holds only the
// messages after the parent session, which parent.json holds.
func (a *FantasyForkAgent) saveSession(name string, messages []fantasy.Message) error {
	if a.sessionDir == "" {
		return nil
	}
	encoded, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return fmt.Errorf("session %s encode failed: %w", name, err)
	}
	err = os.WriteFile(filepath.Join(a.sessionDir, name+".json"), append(encoded, '\n'), 0o600)
	if err != nil {
		return fmt.Errorf("session %s write failed: %w", name, err)
	}
	return nil
}

// Command reviewrules reviews a git diff against the rules in the repo's rule folders, with one LLM fork per
// rule and per correctness hypothesis, all forked from a parent session that holds the diff.
// Exit code: 0 pass, 1 review failed, 2 the tool could not run.
package main

//go:generate go run ./internal/reviewcli/readmetables .

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/jessevdk/go-flags"

	"github.com/popoffvg/reviewrules/internal/agentplugin"
	"github.com/popoffvg/reviewrules/internal/agentplugin/claudeplugin"
	"github.com/popoffvg/reviewrules/internal/agentplugin/codexplugin"
	"github.com/popoffvg/reviewrules/internal/prompts"
	"github.com/popoffvg/reviewrules/internal/review"
	"github.com/popoffvg/reviewrules/internal/reviewcli"
	"github.com/popoffvg/reviewrules/internal/reviewsettings"
	"github.com/popoffvg/reviewrules/internal/rulecatalog"
	"github.com/popoffvg/reviewrules/internal/ruleset"
)

func main() {
	os.Exit(runReviewRules())
}

func runReviewRules() int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "reviewrules")
	commandLine, err := reviewcli.NewReviewCommandLine()
	if err != nil {
		logger.Error("reviewrules command line setup failed", "error", err)
		return 2
	}
	_, err = commandLine.Parser.Parse()
	if flags.WroteHelp(err) {
		return 0
	}
	if err != nil {
		return 2
	}
	options := commandLine.Flags

	root, err := filepath.Abs(options.Repo)
	if err != nil {
		logger.Error("reviewrules repo path invalid", "error", err)
		return 2
	}
	userCase := reviewsettings.UserConfigCaseFromEnv(os.Getenv("CI"))
	config, err := reviewsettings.LoadReviewConfig(options.Config, userCase, root)
	if err != nil {
		logger.Error("reviewrules config load failed", "error", err)
		return 2
	}
	ruleSetCacheDir, err := ruleset.RuleSetCacheDir()
	if err != nil && len(config.Repo.RuleSets) > 0 {
		logger.Error("reviewrules rule set cache dir failed", "error", err)
		return 2
	}
	agentPrompts, err := prompts.Load(config.User.PromptsDir)
	if err != nil {
		logger.Error("reviewrules prompts load failed", "error", err)
		return 2
	}
	if options.PrintConfig {
		err = config.WriteMergedConfig(os.Stdout, cachedRuleSetCommits(ruleSetCacheDir, config.Repo.RuleSets))
		if err != nil {
			logger.Error("reviewrules config print failed", "error", err)
			return 2
		}
		return 0
	}
	failOn, err := rulecatalog.ParseSeverity(config.Repo.FailOn)
	if err != nil {
		logger.Error("reviewrules repo settings fail_on invalid", "error", err)
		return 2
	}
	userConfig := config.User
	newAgent, err := newReviewForkAgentFactory(userConfig, root, agentPrompts)
	if err != nil {
		logger.Error("reviewrules agent setup failed", "error", err)
		return 2
	}
	logger.Info("reviewrules user config", "case", userCase, "agent", userConfig.Agent, "model", userConfig.Model, "hypothesis_model", userConfig.HypothesisModel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ruleSets, err := ruleset.FetchRuleSets(ctx, logger, ruleSetCacheDir, config.Repo.RuleSets)
	if err != nil {
		logger.Error("reviewrules rule set fetch failed", "error", err)
		return 2
	}
	report, err := review.RunReview(ctx, logger, newAgent, review.ReviewRunConfig{
		RepoRoot:                 root,
		Base:                     options.Base,
		IncludeWorktree:          options.Worktree,
		MaxDiffBytes:             config.Repo.MaxDiffBytes,
		ModelName:                userConfig.Model,
		LargeDiffModel:           userConfig.LargeDiffModel,
		LargeDiffBytes:           userConfig.LargeDiffBytes,
		HypothesisModelName:      userConfig.HypothesisModel,
		HypothesisCheckModelName: userConfig.HypothesisCheckModel,
		ReasoningEffort:          userConfig.ReasoningEffort,
		RuleReasoningEffort:      userConfig.RuleReasoningEffort,
		RuleFolders:              config.Repo.RuleFolders,
		RuleSets:                 ruleSets,
		Parallel:                 userConfig.Parallel,
		ParentMaxSteps:           userConfig.ParentSteps,
		ForkMaxSteps:             userConfig.ForkSteps,
		CorrectnessEnabled:       config.Repo.Correctness,
		Prompts:                  agentPrompts,
	})
	if err != nil {
		logger.Error("reviewrules run failed", "error", err)
		return 2
	}

	report.Cost = review.EstimateReviewCost(report, userConfig.Prices)

	jsonTarget := reviewJSONTarget(options.JSONOut, os.Getenv("CI"))
	summaryOut := os.Stdout
	if jsonTarget != "" {
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			logger.Error("reviewrules report encode failed", "error", err)
			return 2
		}
		if jsonTarget == jsonToStdout {
			fmt.Println(string(encoded))
			summaryOut = os.Stderr
		} else {
			err = os.WriteFile(jsonTarget, append(encoded, '\n'), 0o600) //nolint:gosec // G703: the path is the operator's own --json-out flag
			if err != nil {
				logger.Error("reviewrules report write failed", "error", err)
				return 2
			}
		}
	}
	printReviewSummary(summaryOut, report)
	reasons := report.ReviewFailureReasons(failOn, config.Repo.FailOnUnanswered)
	if len(reasons) > 0 {
		_, _ = fmt.Fprintln(summaryOut, "\nReview failed because:")
		for _, reason := range reasons {
			_, _ = fmt.Fprintln(summaryOut, "  - "+reason)
		}
	}
	if len(reasons) > 0 {
		return 1
	}
	return 0
}

// newReviewForkAgentFactory picks the backend the user config names: an API backend, then a built-in
// agent plugin, then the exec plugin of that name in the plugin folder.
func newReviewForkAgentFactory(settings reviewsettings.UserConfig, repoRoot string, text prompts.Set) (review.ReviewForkAgentFactory, error) {
	for _, endpoint := range reviewEndpoints() {
		if endpoint.agent == settings.Agent {
			return newEndpointAgentFactory(settings, endpoint, repoRoot, text)
		}
	}
	plugin, err := selectAgentPlugin(settings)
	if err != nil {
		return nil, err
	}
	return func(promptCacheKey string) review.ReviewForkAgent {
		return agentplugin.NewPluginForkAgent(plugin, repoRoot, promptCacheKey, text.ReviewSystem)
	}, nil
}

// cachedRuleSetCommits maps each rule set name to the commit of its last cached checkout.
// --print-config does not fetch, so a branch may have moved since.
func cachedRuleSetCommits(cacheDir string, sets ruleset.RuleSetList) map[string]string {
	commits := map[string]string{}
	if cacheDir == "" {
		return commits
	}
	for _, set := range sets {
		if commit := ruleset.CachedRuleSetCommit(cacheDir, set); commit != "" {
			commits[set.Name] = commit
		}
	}
	return commits
}

// builtinAgentPlugin is one row of the agent plugins compiled into reviewrules.
type builtinAgentPlugin struct {
	agent     reviewsettings.ReviewAgentKind
	newPlugin func(settings reviewsettings.UserConfig) agentplugin.AgentPlugin
}

func builtinAgentPlugins() []builtinAgentPlugin {
	return []builtinAgentPlugin{
		{agent: reviewsettings.ReviewAgentKindCodex, newPlugin: func(s reviewsettings.UserConfig) agentplugin.AgentPlugin {
			return codexplugin.NewCodexAgentPlugin(s.CodexBinary)
		}},
		{agent: reviewsettings.ReviewAgentKindClaude, newPlugin: func(s reviewsettings.UserConfig) agentplugin.AgentPlugin {
			return claudeplugin.NewClaudeAgentPlugin(s.ClaudeBinary)
		}},
	}
}

// selectAgentPlugin returns the built-in plugin named settings.Agent, else the exec plugin in settings.PluginDir.
func selectAgentPlugin(settings reviewsettings.UserConfig) (agentplugin.AgentPlugin, error) {
	for _, builtin := range builtinAgentPlugins() {
		if builtin.agent == settings.Agent {
			return builtin.newPlugin(settings), nil
		}
	}
	return agentplugin.FindExecAgentPlugin(settings.PluginDir, string(settings.Agent))
}

// newEndpointAgentFactory builds the fantasy agent for one API backend.
func newEndpointAgentFactory(
	settings reviewsettings.UserConfig,
	endpoint reviewEndpoint,
	repoRoot string,
	text prompts.Set,
) (review.ReviewForkAgentFactory, error) {
	baseURL := endpoint.baseURL
	if baseURL == "" || settings.BaseURL != reviewsettings.DefaultUserConfig().BaseURL {
		baseURL = settings.BaseURL
	}
	apiKey := os.Getenv(endpoint.apiKeyEnv)
	if apiKey == "" && endpoint.agent != reviewsettings.ReviewAgentKindFantasy {
		return nil, fmt.Errorf("reviewrules agent %s needs %s", endpoint.agent, endpoint.apiKeyEnv)
	}
	if apiKey == "" {
		// LiteLLM without a master key accepts any bearer token.
		apiKey = "sk-local"
	}
	provider, err := endpoint.newProvider(baseURL, apiKey)
	if err != nil {
		return nil, err
	}
	return func(promptCacheKey string) review.ReviewForkAgent {
		return review.NewFantasyForkAgent(provider, repoRoot, endpoint.callOptions(promptCacheKey), text)
	}, nil
}

// reviewEndpoint is one row of the API backends the fantasy agent code serves.
type reviewEndpoint struct {
	agent       reviewsettings.ReviewAgentKind
	baseURL     string
	apiKeyEnv   string
	newProvider func(baseURL, apiKey string) (fantasy.Provider, error)
	callOptions func(promptCacheKey string) review.FantasyCallOptions
}

// reviewEndpoints lists the API backends; an empty baseURL means the settings' base_url.
// apiKeyEnv holds the name of an environment variable, not a key.
func reviewEndpoints() []reviewEndpoint {
	return []reviewEndpoint{
		{ //nolint:gosec // G101: apiKeyEnv is a variable name, not a credential
			agent: reviewsettings.ReviewAgentKindFantasy, baseURL: "", apiKeyEnv: "LITELLM_API_KEY",
			newProvider: newOpenAICompatProvider, callOptions: liteLLMCallOptions,
		},
		{ //nolint:gosec // G101: apiKeyEnv is a variable name, not a credential
			agent: reviewsettings.ReviewAgentKindOpenAI, baseURL: "https://api.openai.com/v1", apiKeyEnv: "OPENAI_API_KEY",
			newProvider: newOpenAICompatProvider, callOptions: openAICallOptions,
		},
		{ //nolint:gosec // G101: apiKeyEnv is a variable name, not a credential
			agent: reviewsettings.ReviewAgentKindOpenRouter, baseURL: "https://openrouter.ai/api/v1", apiKeyEnv: "OPENROUTER_API_KEY",
			newProvider: newOpenAICompatProvider, callOptions: openAICallOptions,
		},
		{ //nolint:gosec // G101: apiKeyEnv is a variable name, not a credential
			agent: reviewsettings.ReviewAgentKindAnthropic, baseURL: "https://api.anthropic.com", apiKeyEnv: "ANTHROPIC_API_KEY",
			newProvider: newAnthropicProvider, callOptions: anthropicCallOptions,
		},
	}
}

func newOpenAICompatProvider(baseURL, apiKey string) (fantasy.Provider, error) {
	return openaicompat.New(openaicompat.WithBaseURL(baseURL), openaicompat.WithAPIKey(apiKey))
}

// newAnthropicProvider sends ANTHROPIC_WORKSPACE_ID as the anthropic-workspace-id header when it is set:
// the API rejects a key that no workspace scopes unless the request names one.
func newAnthropicProvider(baseURL, apiKey string) (fantasy.Provider, error) {
	options := []anthropic.Option{anthropic.WithBaseURL(baseURL), anthropic.WithAPIKey(apiKey)}
	if workspaceID := os.Getenv("ANTHROPIC_WORKSPACE_ID"); workspaceID != "" {
		options = append(options, anthropic.WithHeaders(map[string]string{"anthropic-workspace-id": workspaceID}))
	}
	return anthropic.New(options...)
}

func liteLLMCallOptions(promptCacheKey string) review.FantasyCallOptions {
	return review.OpenAICompatCallOptions(promptCacheKey, true)
}

func openAICallOptions(promptCacheKey string) review.FantasyCallOptions {
	return review.OpenAICompatCallOptions(promptCacheKey, false)
}

func anthropicCallOptions(string) review.FantasyCallOptions {
	return review.AnthropicCallOptions()
}

func usageTokens(usage fantasy.Usage) string {
	return fmt.Sprintf(
		"input=%d cache_read=%d cache_write=%d output=%d",
		usage.InputTokens,
		usage.CacheReadTokens,
		usage.CacheCreationTokens,
		usage.OutputTokens,
	)
}

// jsonToStdout is the --json-out value that sends the JSON report to stdout.
const jsonToStdout = "-"

// reviewJSONTarget returns where the JSON report goes: the --json-out value, stdout when CI=true
// and the flag is unset, or "" for no JSON (a local run prints only the summary).
func reviewJSONTarget(jsonOutPath string, ciEnv string) string {
	if jsonOutPath != "" {
		return jsonOutPath
	}
	if ciEnv == "true" {
		return jsonToStdout
	}
	return ""
}

// printReviewSummary writes one line per rule and hypothesis. It goes to stderr only when the
// JSON report takes stdout, so stdout stays pure JSON.
func printReviewSummary(target *os.File, report review.ReviewReport) {
	var out strings.Builder
	for _, rule := range report.Rules {
		verdict := string(rule.Verdict)
		if rule.Verdict == review.RuleVerdictError {
			verdict = "WARN"
		}
		fmt.Fprintf(&out, "%-6s %-7s %s %s\n", verdict, rule.Severity, rule.RuleID, rule.Error)
		for _, finding := range rule.Findings {
			fmt.Fprintf(&out, "       %s:%d — %s\n", finding.File, finding.Line, finding.Message)
		}
	}
	if report.Correctness.GeneratorError != "" {
		fmt.Fprintf(&out, "WARN   hypothesis generator gave no answer: %s\n", report.Correctness.GeneratorError)
	}
	for _, check := range report.Correctness.HypothesisCheck {
		fmt.Fprintf(&out, "%-9s %s %s — %s\n", check.Verdict, check.ID, check.At, check.Question)
		if check.Verdict == review.HypothesisVerdictConfirmed {
			fmt.Fprintf(&out, "          input that breaks it: %s\n", check.Scenario)
			for _, evidence := range check.Evidence {
				fmt.Fprintf(&out, "          evidence: %s\n", evidence)
			}
			if check.Edit != "" {
				fmt.Fprintf(&out, "          fix: %s\n", check.Edit)
			}
		}
	}
	fmt.Fprintf(&out, "usage: total %s\n", usageTokens(report.TotalUsage))
	for _, part := range report.Cost.Parts {
		cost := "no price"
		if part.Priced {
			cost = fmt.Sprintf("$%.4f", part.USD)
		}
		fmt.Fprintf(&out, "usage: %-11s %s %s cost=%s\n", part.Part, part.Model, usageTokens(part.Usage), cost)
	}
	fmt.Fprintf(&out, "cost estimate: total $%.4f\n", report.Cost.TotalUSD)
	for _, model := range report.Cost.UnpricedModels {
		fmt.Fprintf(&out, "cost estimate: %s has no price; add a prices.%q table to the user config\n", model, model)
	}
	_, _ = fmt.Fprint(target, out.String())
}

// Package reviewsettings loads the two config files of a run: the repo settings
// .reviewrules/settings.toml (team policy) and the user config ~/.config/reviewrules/config.toml
// (machine choices, one table per run case). Each key has one owner file; ConfigKeys lists them.
package reviewsettings

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/popoffvg/reviewrules/internal/review"
)

// ReviewAgentKind names the backend that runs the parent session and the forks. A name that is not
// an API backend or a built-in agent plugin selects the exec plugin `reviewrules-agent-<name>` in PluginDir.
type ReviewAgentKind string

// ReviewAgentKind values; ReviewAgentKindUnknown is the zero value and fails validation.
const (
	ReviewAgentKindUnknown ReviewAgentKind = ""
	// ReviewAgentKindFantasy calls an OpenAI-compatible endpoint, a LiteLLM proxy in practice.
	ReviewAgentKindFantasy ReviewAgentKind = "fantasy"
	// ReviewAgentKindClaude is the built-in agent plugin that runs the local claude binary.
	ReviewAgentKindClaude ReviewAgentKind = "claude"
	// ReviewAgentKindCodex is the built-in agent plugin that runs the local codex binary.
	ReviewAgentKindCodex ReviewAgentKind = "codex"
	// ReviewAgentKindOpenAI calls the OpenAI API directly with OPENAI_API_KEY.
	ReviewAgentKindOpenAI ReviewAgentKind = "openai"
	// ReviewAgentKindOpenRouter calls OpenRouter directly with OPENROUTER_API_KEY.
	ReviewAgentKindOpenRouter ReviewAgentKind = "openrouter"
	// ReviewAgentKindAnthropic calls the Anthropic API directly with ANTHROPIC_API_KEY.
	ReviewAgentKindAnthropic ReviewAgentKind = "anthropic"
)

// UserConfigCase selects the table of the user config.
type UserConfigCase string

// UserConfigCase values: [local] for a laptop run, [ci] when CI=true.
const (
	UserConfigCaseLocal UserConfigCase = "local"
	UserConfigCaseCI    UserConfigCase = "ci"
)

// UserConfig is one case of the user config. Model names are the backend's own:
// LiteLLM model names for fantasy, claude model aliases for claude, codex model ids for codex.
// The toml, description, and default-text tags of a field make its ConfigKey row.
type UserConfig struct {
	Agent                ReviewAgentKind                   `toml:"agent"                  description:"backend: fantasy (LiteLLM endpoint), openai, openrouter, or anthropic (direct API), claude or codex (built-in plugins), or an exec plugin name"`
	BaseURL              string                            `toml:"base_url"               description:"endpoint for fantasy; for openai, openrouter, and anthropic only when it differs from the default LiteLLM URL"`
	Model                string                            `toml:"model"                  description:"model for the parent session, the rule forks, and the hypothesis checkers"`
	Profiles             []review.ReviewProfile            `toml:"profiles"               description:"ordered [[<case>.profiles]] with if (an expr-lang expression over diff_bytes and changed_files; empty matches every diff), action (rules, hypothesis, or hypothesis_check), model, and optional reasoning_effort; for each action the first profile from the top whose if holds picks its model" default-text:"none"`
	HypothesisModel      string                            `toml:"hypothesis_model"       description:"model that writes the correctness hypotheses"`
	HypothesisCheckModel string                            `toml:"hypothesis_check_model" description:"model that answers each hypothesis; empty uses model"                                                                        default-text:"model"`
	ReasoningEffort      string                            `toml:"reasoning_effort"       description:"effort for the parent session and the hypothesis forks"`
	RuleReasoningEffort  string                            `toml:"rule_reasoning_effort"  description:"effort for the rule forks; at medium a small model skips checks at random"`
	Parallel             int                               `toml:"parallel"               description:"forks running at the same time"`
	ParentSteps          int                               `toml:"parent_steps"           description:"tool-call steps for the parent session"`
	ForkSteps            int                               `toml:"fork_steps"             description:"tool-call steps for each fork (not used by codex)"`
	PluginDir            string                            `toml:"plugin_dir"             description:"folder of the exec agent plugins, named reviewrules-agent-<agent>"                                                         default-text:"~/.config/reviewrules/plugins"`
	PromptsDir           string                            `toml:"prompts_dir"            description:"folder of prompt files that replace the built-in agent prompts; a file keeps its built-in name; empty uses the built-in prompts" default-text:"built-in prompts"`
	ClaudeBinary         string                            `toml:"claude_binary"          description:"claude binary for the claude agent"`
	CodexBinary          string                            `toml:"codex_binary"           description:"codex binary for the codex agent"`
	Prices               map[string]review.ModelTokenPrice `toml:"prices"                 description:"per-model price table [<case>.prices.\"<model>\"] with input, cached, cache_write, output in USD per 1M tokens, and optional long_prompt_tokens with a long_prompt table for larger requests; an entry replaces the built-in price" default-text:"built in for the default models"`
}

type userConfigFile struct {
	Local UserConfig `toml:"local"`
	CI    UserConfig `toml:"ci"`
}

// DefaultUserConfig is what a run uses with no user config file.
func DefaultUserConfig() UserConfig {
	return UserConfig{
		Agent:               ReviewAgentKindFantasy,
		BaseURL:             "http://localhost:4000/v1",
		Model:               "gpt-6-luna",
		HypothesisModel:     "gpt-6.1-sol",
		ReasoningEffort:     "medium",
		RuleReasoningEffort: "high",
		Parallel:            8,
		ParentSteps:         8,
		ForkSteps:           8,
		PluginDir:           filepath.Join(reviewrulesConfigDir(), "plugins"),
		ClaudeBinary:        "claude",
		CodexBinary:         "codex",
		Prices:              review.DefaultModelTokenPrices(),
	}
}

// DefaultUserConfigPath is $XDG_CONFIG_HOME/reviewrules/config.toml, else ~/.config/reviewrules/config.toml.
func DefaultUserConfigPath() string {
	return filepath.Join(reviewrulesConfigDir(), "config.toml")
}

// reviewrulesConfigDir is $XDG_CONFIG_HOME/reviewrules, else ~/.config/reviewrules.
func reviewrulesConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "reviewrules")
}

// UserConfigCaseFromEnv returns the ci case when the CI environment variable is "true".
func UserConfigCaseFromEnv(ciEnv string) UserConfigCase {
	if ciEnv == "true" {
		return UserConfigCaseCI
	}
	return UserConfigCaseLocal
}

func (c UserConfig) validate() error {
	if c.Agent == ReviewAgentKindUnknown {
		return errors.New("user config agent is empty")
	}
	return review.CheckReviewProfiles(c.Profiles)
}

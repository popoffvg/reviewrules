package main

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/popoffvg/reviewrules/internal/reviewsettings"
)

func TestDetectReviewAgentPicksTheOneBackendOnTheMachine(t *testing.T) {
	cases := []struct {
		name     string
		agent    reviewsettings.ReviewAgentKind
		binaries []string
		envs     []string
		want     reviewsettings.AgentDefaults
	}{
		{
			name:     "only-claude-in-path",
			binaries: []string{"claude"},
			want:     reviewsettings.AgentDefaults{Agent: "claude", Source: "detected: claude in PATH"},
		},
		{
			name: "only-litellm-key-set",
			envs: []string{"LITELLM_API_KEY"},
			want: reviewsettings.AgentDefaults{Agent: "fantasy", Model: "gpt-6-luna", HypothesisModel: "gpt-6.1-sol", Source: "detected: LITELLM_API_KEY is set"},
		},
		{
			name:     "set-agent-skips-detection-and-gives-its-models",
			agent:    "openai",
			binaries: []string{"claude", "codex"},
			want:     reviewsettings.AgentDefaults{Agent: "openai", Model: "gpt-6-luna", HypothesisModel: "gpt-6.1-sol", Source: "default of agent openai"},
		},
		{
			name:  "set-exec-plugin-agent-has-no-models",
			agent: "pi",
			want:  reviewsettings.AgentDefaults{Agent: "pi"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := reviewsettings.DefaultUserConfig()
			settings.Agent = tc.agent

			got, err := detectReviewAgent(settings, fakeAgentProbe(tc.binaries, tc.envs), "/home/me/config.toml", reviewsettings.UserConfigCaseLocal)

			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("defaults = %+v; want %+v", got, tc.want)
			}
		})
	}
}

func TestDetectReviewAgentFailsWithoutOneChoice(t *testing.T) {
	cases := []struct {
		name     string
		binaries []string
		envs     []string
		wantErr  string
	}{
		{
			name:    "nothing-found",
			wantErr: `detect review agent: no agent found; install codex, install claude, set LITELLM_API_KEY, set OPENAI_API_KEY, set OPENROUTER_API_KEY, or set agent in [local] of /home/me/config.toml`,
		},
		{
			name:     "claude-and-openai-key",
			binaries: []string{"claude"},
			envs:     []string{"OPENAI_API_KEY"},
			wantErr:  `detect review agent: several agents found (claude in PATH, OPENAI_API_KEY is set); choose one and write it to /home/me/config.toml, for example: [local] agent = "claude"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := reviewsettings.DefaultUserConfig()

			_, err := detectReviewAgent(settings, fakeAgentProbe(tc.binaries, tc.envs), "/home/me/config.toml", reviewsettings.UserConfigCaseLocal)

			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("error = %v; want %q", err, tc.wantErr)
			}
		})
	}
}

func TestWithAgentDefaultsKeepsTheModelsTheUserSet(t *testing.T) {
	config := reviewsettings.DefaultReviewConfig()
	config.User.Model = "opus"
	defaults := reviewsettings.AgentDefaults{Agent: "fantasy", Model: "gpt-6-luna", HypothesisModel: "gpt-6.1-sol", Source: "detected: LITELLM_API_KEY is set"}

	got := config.WithAgentDefaults(defaults)

	if got.User.Agent != "fantasy" || got.User.Model != "opus" || got.User.HypothesisModel != "gpt-6.1-sol" {
		t.Errorf("user config = %+v; want agent fantasy, model opus, hypothesis model gpt-6.1-sol", got.User)
	}
	var printed strings.Builder
	err := got.WriteMergedConfig(&printed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(printed.String(), "# detected: LITELLM_API_KEY is set") {
		t.Errorf("--print-config output has no detected source:\n%s", printed.String())
	}
}

func fakeAgentProbe(binaries []string, envs []string) reviewAgentProbe {
	return reviewAgentProbe{
		lookPath: func(file string) (string, error) {
			if slices.Contains(binaries, file) {
				return "/usr/bin/" + file, nil
			}
			return "", errors.New("not in PATH")
		},
		getenv: func(key string) string {
			if slices.Contains(envs, key) {
				return "value"
			}
			return ""
		},
	}
}

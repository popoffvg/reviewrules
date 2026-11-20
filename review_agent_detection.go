package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/popoffvg/reviewrules/internal/reviewsettings"
)

// reviewAgentProbe is what agent detection reads from the machine.
type reviewAgentProbe struct {
	lookPath func(file string) (string, error)
	getenv   func(key string) string
}

func machineAgentProbe() reviewAgentProbe {
	return reviewAgentProbe{lookPath: exec.LookPath, getenv: os.Getenv}
}

// detectReviewAgent returns the values of the agent keys that the user config leaves unset. With
// agent set, they are the default models of that agent, and the machine is not probed. With agent
// unset, it picks the one backend found on the machine: a built-in plugin binary in PATH, or the API
// key variable of an endpoint. None found, or several found, is an error that tells the user to set
// agent in the user config.
func detectReviewAgent(
	settings reviewsettings.UserConfig,
	probe reviewAgentProbe,
	userConfigPath string,
	userCase reviewsettings.UserConfigCase,
) (reviewsettings.AgentDefaults, error) {
	candidates := reviewAgentCandidates(settings)
	if settings.Agent != reviewsettings.ReviewAgentKindUnknown {
		return chosenAgentDefaults(settings.Agent, candidates), nil
	}

	var found []reviewAgentCandidate
	var hints []string
	for _, candidate := range candidates {
		hints = append(hints, candidate.missingHint)
		if candidate.isOnMachine(probe) {
			found = append(found, candidate)
		}
	}
	if len(found) == 0 {
		return reviewsettings.AgentDefaults{}, fmt.Errorf(
			"detect review agent: no agent found; %s, or set agent in [%s] of %s",
			strings.Join(hints, ", "),
			userCase,
			userConfigPath,
		)
	}
	if len(found) > 1 {
		var presences []string
		for _, candidate := range found {
			presences = append(presences, candidate.presence)
		}
		return reviewsettings.AgentDefaults{}, fmt.Errorf(
			"detect review agent: several agents found (%s); choose one and write it to %s, for example: [%s] agent = %q",
			strings.Join(presences, ", "),
			userConfigPath,
			userCase,
			found[0].defaults.Agent,
		)
	}
	return found[0].defaults, nil
}

// reviewAgentCandidate is one backend that agent detection can find on the machine.
type reviewAgentCandidate struct {
	defaults    reviewsettings.AgentDefaults
	isOnMachine func(probe reviewAgentProbe) bool
	presence    string
	missingHint string
}

// reviewAgentCandidates turns the built-in plugin rows and the endpoint rows into candidates, in row order.
func reviewAgentCandidates(settings reviewsettings.UserConfig) []reviewAgentCandidate {
	var candidates []reviewAgentCandidate
	for _, plugin := range builtinAgentPlugins() {
		binary := plugin.binary(settings)
		presence := binary + " in PATH"
		candidates = append(candidates, reviewAgentCandidate{
			defaults: reviewsettings.AgentDefaults{
				Agent:           plugin.agent,
				Model:           plugin.model,
				HypothesisModel: plugin.hypothesisModel,
				Source:          "detected: " + presence,
			},
			isOnMachine: func(probe reviewAgentProbe) bool {
				_, err := probe.lookPath(binary)
				return err == nil
			},
			presence:    presence,
			missingHint: "install " + binary,
		})
	}
	for _, endpoint := range reviewEndpoints() {
		presence := endpoint.apiKeyEnv + " is set"
		candidates = append(candidates, reviewAgentCandidate{
			defaults: reviewsettings.AgentDefaults{
				Agent:           endpoint.agent,
				Model:           endpoint.model,
				HypothesisModel: endpoint.hypothesisModel,
				Source:          "detected: " + presence,
			},
			isOnMachine: func(probe reviewAgentProbe) bool { return probe.getenv(endpoint.apiKeyEnv) != "" },
			presence:    presence,
			missingHint: "set " + endpoint.apiKeyEnv,
		})
	}
	return candidates
}

// chosenAgentDefaults gives the default models of the agent the user config names; an exec plugin has none.
func chosenAgentDefaults(
	agent reviewsettings.ReviewAgentKind,
	candidates []reviewAgentCandidate,
) reviewsettings.AgentDefaults {
	for _, candidate := range candidates {
		if candidate.defaults.Agent == agent {
			defaults := candidate.defaults
			defaults.Source = "default of agent " + string(agent)
			return defaults
		}
	}
	return reviewsettings.AgentDefaults{Agent: agent}
}

package review

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// ProfileAction names the forks whose model a profile picks.
type ProfileAction string

// ProfileAction values.
const (
	// ProfileActionRules is the parent session and the rule forks; they share one model, so the
	// forks read the parent's prompt cache.
	ProfileActionRules ProfileAction = "rules"
	// ProfileActionHypothesis is the hypothesis generator fork.
	ProfileActionHypothesis ProfileAction = "hypothesis"
	// ProfileActionHypothesisCheck is the fork that answers the hypotheses.
	ProfileActionHypothesisCheck ProfileAction = "hypothesis_check"
)

var profileActions = []ProfileAction{ProfileActionRules, ProfileActionHypothesis, ProfileActionHypothesisCheck}

// ReviewProfile gives one action its model when If holds for the diff. For each action, the first
// profile from the top whose If holds wins; an empty If holds for every diff.
type ReviewProfile struct {
	If              string        `json:"if,omitempty"               toml:"if"`
	Action          ProfileAction `json:"action"                     toml:"action"`
	Model           string        `json:"model"                      toml:"model"`
	ReasoningEffort string        `json:"reasoning_effort,omitempty" toml:"reasoning_effort"`
}

// ProfileDiff is what an If expression reads, in expr-lang syntax: `diff_bytes >= 65536 && changed_files > 10`.
type ProfileDiff struct {
	DiffBytes    int `expr:"diff_bytes"`
	ChangedFiles int `expr:"changed_files"`
}

// reviewChoices is the model and the reasoning effort of every action in this run; MaxSteps stays 0.
type reviewChoices struct {
	Rules           ForkSettings
	Hypothesis      ForkSettings
	HypothesisCheck ForkSettings
}

type compiledProfile struct {
	ReviewProfile
	// condition is nil when If is empty.
	condition *vm.Program
}

// CheckReviewProfiles reports the first profile with an unknown action, an empty model, or an If
// that does not compile to a condition, so a bad profile stops the run when the config loads.
func CheckReviewProfiles(profiles []ReviewProfile) error {
	_, err := compileProfiles(profiles)
	return err
}

func compileProfiles(profiles []ReviewProfile) ([]compiledProfile, error) {
	compiled := make([]compiledProfile, 0, len(profiles))
	for index, profile := range profiles {
		if !slices.Contains(profileActions, profile.Action) {
			return nil, fmt.Errorf("review profile %d: action %q is not one of %v", index+1, profile.Action, profileActions)
		}
		if profile.Model == "" {
			return nil, fmt.Errorf("review profile %d: model is empty", index+1)
		}
		entry := compiledProfile{ReviewProfile: profile}
		if profile.If != "" {
			program, err := expr.Compile(profile.If, expr.Env(ProfileDiff{}), expr.AsBool())
			if err != nil {
				return nil, fmt.Errorf("review profile %d: if %q: %w", index+1, profile.If, err)
			}
			entry.condition = program
		}
		compiled = append(compiled, entry)
	}
	return compiled, nil
}

// matchProfile returns the first profile from the top with this action whose If holds for the diff.
func matchProfile(profiles []compiledProfile, action ProfileAction, diff ProfileDiff) (ReviewProfile, bool, error) {
	for index, profile := range profiles {
		if profile.Action != action {
			continue
		}
		if profile.condition == nil {
			return profile.ReviewProfile, true, nil
		}
		holds, err := expr.Run(profile.condition, diff)
		if err != nil {
			return ReviewProfile{}, false, fmt.Errorf("review profile %d: if %q: %w", index+1, profile.If, err)
		}
		if holds.(bool) {
			return profile.ReviewProfile, true, nil
		}
	}
	return ReviewProfile{}, false, nil
}

// chooseActions picks the model and effort of each action for the diff. With no matching profile,
// an action keeps the case keys; an empty hypothesis model follows the rules model, so its fork
// reads the parent's cache.
func (cfg ReviewRunConfig) chooseActions(diff ProfileDiff) (reviewChoices, error) {
	profiles, err := compileProfiles(cfg.Profiles)
	if err != nil {
		return reviewChoices{}, err
	}
	choose := func(action ProfileAction, fallback ForkSettings) (ForkSettings, error) {
		profile, ok, err := matchProfile(profiles, action, diff)
		if err != nil || !ok {
			return fallback, err
		}
		return ForkSettings{Model: profile.Model, ReasoningEffort: cmp.Or(profile.ReasoningEffort, fallback.ReasoningEffort)}, nil
	}
	rules, err := choose(ProfileActionRules, ForkSettings{
		Model: cfg.ModelName, ReasoningEffort: cmp.Or(cfg.RuleReasoningEffort, cfg.ReasoningEffort),
	})
	if err != nil {
		return reviewChoices{}, err
	}
	hypothesis, err := choose(ProfileActionHypothesis, ForkSettings{
		Model: cmp.Or(cfg.HypothesisModelName, rules.Model), ReasoningEffort: cfg.ReasoningEffort,
	})
	if err != nil {
		return reviewChoices{}, err
	}
	check, err := choose(ProfileActionHypothesisCheck, ForkSettings{
		Model: cmp.Or(cfg.HypothesisCheckModelName, rules.Model), ReasoningEffort: cfg.ReasoningEffort,
	})
	if err != nil {
		return reviewChoices{}, err
	}
	return reviewChoices{Rules: rules, Hypothesis: hypothesis, HypothesisCheck: check}, nil
}

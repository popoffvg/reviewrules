package review

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"charm.land/fantasy"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

// hypothesisHunt is one row of the generator's hunt table. Group names the generator fork that walks
// the row: one fork per group, so a group that finds a question does not cut the walk of another.
type hypothesisHunt struct {
	Group string
	prompts.Hunt
}

var hypothesisHunts = []hypothesisHunt{
	{Group: "data", Hunt: prompts.Hunt{Name: "Boundaries", LookFor: "off-by-one, inclusive vs exclusive end, empty collection, first and last item"}},
	{Group: "data", Hunt: prompts.Hunt{Name: "Absent values", LookFor: "nil, empty string, zero, missing map key, an optional not set"}},
	{Group: "failure", Hunt: prompts.Hunt{Name: "Error paths", LookFor: "an error dropped, logged and ignored, or turned into a success value; a state set before a step that throws and never cleared"}},
	{Group: "failure", Hunt: prompts.Hunt{Name: "Callers", LookFor: "a changed signature, return value, or meaning whose callers were not updated"}},
	{Group: "failure", Hunt: prompts.Hunt{Name: "Pairs", LookFor: "an open, register, lock, or mark whose close runs in a hook or callback of another layer; an exit of a caller up the stack that skips it"}},
	{Group: "interleaving", Hunt: prompts.Hunt{Name: "Concurrency", LookFor: "a new shared value read or written from two goroutines, callbacks, or queued tasks with no guard; a value read before an await and used after it"}},
	{Group: "interleaving", Hunt: prompts.Hunt{Name: "Ownership", LookFor: "a token, ID, or cursor that one owner (queue, subscription, process, user, session) issues and that another owner can pass back"}},
	{Group: "interleaving", Hunt: prompts.Hunt{Name: "Misuse", LookFor: "the likely caller mistake with a new value (`>` for `>=`, a stale value, a value of another owner), and whether it fails as a skip or as a duplicate"}},
}

// generateHypothesesByHuntGroup runs one generator fork per hunt group in parallel, each with the full
// cap. A failed group keeps the questions of the other groups; its error is joined into the result.
func generateHypothesesByHuntGroup(
	ctx context.Context,
	agent ReviewForkAgent,
	limit chan struct{},
	settings ForkSettings,
	text prompts.Set,
	input prompts.HypothesisGeneratorInput,
) ([]Hypothesis, fantasy.Usage, error) {
	groups := hypothesisHuntGroups()
	replies := make([][]Hypothesis, len(groups))
	usages := make([]fantasy.Usage, len(groups))
	errs := make([]error, len(groups))
	var wait sync.WaitGroup
	for index, group := range groups {
		wait.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			groupInput := input
			groupInput.Hunts = group.Hunts
			replies[index], usages[index], errs[index] = GenerateHypothesesInFork(ctx, agent, settings, text, groupInput)
			if errs[index] != nil {
				errs[index] = fmt.Errorf("hunt group %s: %w", group.Name, errs[index])
			}
		})
	}
	wait.Wait()
	var generated []Hypothesis
	var usage fantasy.Usage
	for index := range groups {
		generated = append(generated, replies[index]...)
		usage = AddUsage(usage, usages[index])
	}
	return generated, usage, errors.Join(errs...)
}

type hypothesisHuntGroup struct {
	Name  string
	Hunts []prompts.Hunt
}

// hypothesisHuntGroups splits hypothesisHunts by Group, in the order of the table.
func hypothesisHuntGroups() []hypothesisHuntGroup {
	var groups []hypothesisHuntGroup
	for _, hunt := range hypothesisHunts {
		last := len(groups) - 1
		if last >= 0 && groups[last].Name == hunt.Group {
			groups[last].Hunts = append(groups[last].Hunts, hunt.Hunt)
			continue
		}
		groups = append(groups, hypothesisHuntGroup{Name: hunt.Group, Hunts: []prompts.Hunt{hunt.Hunt}})
	}
	return groups
}

// allHypothesisHunts is every row of the table, for the prompt text that keys the hypothesis cache.
func allHypothesisHunts() []prompts.Hunt {
	hunts := make([]prompts.Hunt, len(hypothesisHunts))
	for index, hunt := range hypothesisHunts {
		hunts[index] = hunt.Hunt
	}
	return hunts
}

package review

import (
	"cmp"
	"slices"

	"charm.land/fantasy"
)

// ModelTokenPrice is a model's price in USD per 1M tokens. CacheWrite prices the input tokens a
// request writes to the provider's prompt cache; it is 0 where the provider does not charge for it.
type ModelTokenPrice struct {
	Input       float64 `json:"input"        toml:"input"`
	CachedInput float64 `json:"cached_input" toml:"cached"`
	CacheWrite  float64 `json:"cache_write"  toml:"cache_write"`
	Output      float64 `json:"output"       toml:"output"`
	// LongPrompt prices a request whose input (uncached, read from the cache, and written to it) is
	// more than LongPromptTokens; nil means one price for every request size.
	LongPromptTokens int64            `json:"long_prompt_tokens,omitempty" toml:"long_prompt_tokens,omitzero"`
	LongPrompt       *ModelTokenPrice `json:"long_prompt,omitempty"        toml:"long_prompt,omitempty"`
}

// DefaultModelTokenPrices lists the per-token prices the cost estimate uses when no prices table of the
// user config replaces them. Prices change; check them before you rely on the estimate.
func DefaultModelTokenPrices() map[string]ModelTokenPrice {
	// Anthropic charges 1.25x input for a 5-minute cache write; Haiku 5.5 charges 5x above 100,000 tokens.
	haiku := ModelTokenPrice{
		Input: 0.10, CachedInput: 0.01, CacheWrite: 0.125, Output: 0.50,
		LongPromptTokens: 100_000,
		LongPrompt:       &ModelTokenPrice{Input: 0.50, CachedInput: 0.05, CacheWrite: 0.625, Output: 2.50},
	}
	sonnet := ModelTokenPrice{Input: 2.00, CachedInput: 0.10, CacheWrite: 2.50, Output: 10.00}
	return map[string]ModelTokenPrice{
		"gpt-6-luna":  {Input: 0.10, CachedInput: 0.01, Output: 0.50},
		"gpt-6.1-sol": {Input: 2.00, CachedInput: 0.10, Output: 10.00},
		// OpenRouter lists these under the openai/ prefix at the same prices.
		"openai/gpt-6-luna":  {Input: 0.10, CachedInput: 0.01, Output: 0.50},
		"openai/gpt-6.1-sol": {Input: 2.00, CachedInput: 0.10, Output: 10.00},
		// claude aliases resolve to Claude Haiku 5.5 and Sonnet 5.5.
		"haiku":  haiku,
		"sonnet": sonnet,
		// The anthropic backend takes full model ids.
		"claude-haiku-5-5":  haiku,
		"claude-sonnet-5-5": sonnet,
		"claude-opus-5-5":   {Input: 4.00, CachedInput: 0.20, CacheWrite: 5.00, Output: 20.00},
	}
}

// ReviewPart names one part of a run in the usage and cost lines.
type ReviewPart string

const (
	// ReviewPartParent is the parent session that reads the diff.
	ReviewPartParent ReviewPart = "parent"
	// ReviewPartRules is every rule fork.
	ReviewPartRules ReviewPart = "rules"
	// ReviewPartCorrectness is the hypothesis generator and every hypothesis checker.
	ReviewPartCorrectness ReviewPart = "correctness"
)

// ReviewPartCost is the usage and the cost of one part of a run on one model.
// USD is 0 when Priced is false.
type ReviewPartCost struct {
	Part   ReviewPart    `json:"part"`
	Model  string        `json:"model"`
	Usage  fantasy.Usage `json:"usage"`
	USD    float64       `json:"usd"`
	Priced bool          `json:"priced"`
	// requests is the usage of each model request of the part; empty when the backend reports only totals.
	requests []fantasy.Usage
}

// ReviewCostEstimate is what the run's tokens would cost at per-token prices, in USD.
// A model with no known price is listed in UnpricedModels and adds nothing to TotalUSD.
type ReviewCostEstimate struct {
	Parts          []ReviewPartCost       `json:"parts"`
	PerPartUSD     map[ReviewPart]float64 `json:"per_part_usd"`
	PerModelUSD    map[string]float64     `json:"per_model_usd"`
	TotalUSD       float64                `json:"total_usd"`
	UnpricedModels []string               `json:"unpriced_models,omitempty"`
}

// reviewPartUsages lists each part of a run with the model it runs on.
func reviewPartUsages(report ReviewReport) []ReviewPartCost {
	hypothesisModel := cmp.Or(report.HypothesisModel, report.Model)
	checkModel := cmp.Or(report.HypothesisCheckModel, report.Model)
	return []ReviewPartCost{
		{Part: ReviewPartParent, Model: report.Model, Usage: report.ParentUsage, requests: report.ParentRequests},
		{Part: ReviewPartRules, Model: report.Model, Usage: report.RulesUsage, requests: ruleRequests(report.Rules)},
		{
			Part: ReviewPartCorrectness, Model: hypothesisModel,
			Usage: report.Correctness.GeneratorUsage, requests: report.Correctness.GeneratorRequests,
		},
		{
			Part: ReviewPartCorrectness, Model: checkModel,
			Usage: report.Correctness.CheckUsage, requests: checkRequests(report.Correctness.HypothesisCheck),
		},
	}
}

// EstimateReviewCost prices each part on its model and merges the rows of one part on one model.
// A part with no usage gets no row. Output tokens include reasoning tokens.
func EstimateReviewCost(report ReviewReport, prices map[string]ModelTokenPrice) ReviewCostEstimate {
	estimate := ReviewCostEstimate{PerPartUSD: map[ReviewPart]float64{}, PerModelUSD: map[string]float64{}}
	for _, row := range reviewPartUsages(report) {
		if row.Usage == (fantasy.Usage{}) {
			continue
		}
		index := slices.IndexFunc(estimate.Parts, func(part ReviewPartCost) bool {
			return part.Part == row.Part && part.Model == row.Model
		})
		if index < 0 {
			estimate.Parts = append(estimate.Parts, row)
			continue
		}
		estimate.Parts[index].Usage = AddUsage(estimate.Parts[index].Usage, row.Usage)
		estimate.Parts[index].requests = append(estimate.Parts[index].requests, row.requests...)
	}
	for index := range estimate.Parts {
		part := &estimate.Parts[index]
		price, ok := prices[part.Model]
		if !ok {
			if !slices.Contains(estimate.UnpricedModels, part.Model) {
				estimate.UnpricedModels = append(estimate.UnpricedModels, part.Model)
			}
			continue
		}
		part.Priced = true
		part.USD = price.partUSD(part.Usage, part.requests)
		estimate.PerPartUSD[part.Part] += part.USD
		estimate.PerModelUSD[part.Model] += part.USD
		estimate.TotalUSD += part.USD
	}
	return estimate
}

// partUSD prices each request at the tier its input size selects. Without requests, it prices the
// total at the base tier: a backend that reports one total per call hides the size of each request.
func (p ModelTokenPrice) partUSD(total fantasy.Usage, requests []fantasy.Usage) float64 {
	if len(requests) == 0 {
		return p.tokensUSD(total)
	}
	usd := 0.0
	for _, request := range requests {
		usd += p.requestTier(request).tokensUSD(request)
	}
	return usd
}

func (p ModelTokenPrice) requestTier(request fantasy.Usage) ModelTokenPrice {
	input := request.InputTokens + request.CacheReadTokens + request.CacheCreationTokens
	if p.LongPrompt == nil || input <= p.LongPromptTokens {
		return p
	}
	return *p.LongPrompt
}

func (p ModelTokenPrice) tokensUSD(usage fantasy.Usage) float64 {
	return (float64(usage.InputTokens)*p.Input +
		float64(usage.CacheReadTokens)*p.CachedInput +
		float64(usage.CacheCreationTokens)*p.CacheWrite +
		float64(usage.OutputTokens)*p.Output) / 1e6
}

func ruleRequests(rules []RuleCheckResult) []fantasy.Usage {
	var requests []fantasy.Usage
	for _, rule := range rules {
		requests = append(requests, rule.Requests...)
	}
	return requests
}

func checkRequests(checks []HypothesisCheck) []fantasy.Usage {
	var requests []fantasy.Usage
	for _, check := range checks {
		requests = append(requests, check.Requests...)
	}
	return requests
}

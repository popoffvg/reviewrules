package review

import (
	"cmp"
	"slices"

	"charm.land/fantasy"
)

// ModelTokenPrice is a model's price in USD per 1M tokens.
type ModelTokenPrice struct {
	Input       float64 `json:"input"        toml:"input"`
	CachedInput float64 `json:"cached_input" toml:"cached"`
	Output      float64 `json:"output"       toml:"output"`
}

// DefaultModelTokenPrices lists the per-token prices the cost estimate uses when no prices table of the
// user config replaces them. Prices change; check them before you rely on the estimate.
func DefaultModelTokenPrices() map[string]ModelTokenPrice {
	haiku := ModelTokenPrice{Input: 0.10, CachedInput: 0.01, Output: 0.50}
	sonnet := ModelTokenPrice{Input: 2.00, CachedInput: 0.20, Output: 10.00}
	return map[string]ModelTokenPrice{
		"gpt-6-luna":  {Input: 0.10, CachedInput: 0.01, Output: 0.50},
		"gpt-6.1-sol": {Input: 2.00, CachedInput: 0.10, Output: 10.00},
		// OpenRouter lists these under the openai/ prefix at the same prices.
		"openai/gpt-6-luna":  {Input: 0.10, CachedInput: 0.01, Output: 0.50},
		"openai/gpt-6.1-sol": {Input: 2.00, CachedInput: 0.10, Output: 10.00},
		// claude aliases resolve to Claude Haiku 5.5 and Sonnet 5.5; cache writes are not priced here.
		"haiku":  haiku,
		"sonnet": sonnet,
		// The anthropic backend takes full model ids.
		"claude-haiku-5-5":  haiku,
		"claude-sonnet-5-5": sonnet,
		"claude-opus-5-5":   {Input: 4.00, CachedInput: 0.20, Output: 20.00},
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
		{Part: ReviewPartParent, Model: report.Model, Usage: report.ParentUsage},
		{Part: ReviewPartRules, Model: report.Model, Usage: report.RulesUsage},
		{Part: ReviewPartCorrectness, Model: hypothesisModel, Usage: report.Correctness.GeneratorUsage},
		{Part: ReviewPartCorrectness, Model: checkModel, Usage: report.Correctness.CheckUsage},
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
		part.USD = float64(part.Usage.InputTokens)*price.Input/1e6 +
			float64(part.Usage.CacheReadTokens)*price.CachedInput/1e6 +
			float64(part.Usage.OutputTokens)*price.Output/1e6
		estimate.PerPartUSD[part.Part] += part.USD
		estimate.PerModelUSD[part.Model] += part.USD
		estimate.TotalUSD += part.USD
	}
	return estimate
}

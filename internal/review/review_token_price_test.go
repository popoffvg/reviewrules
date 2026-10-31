package review

import (
	"math"
	"testing"

	"charm.land/fantasy"
)

func TestEstimateReviewCost(t *testing.T) {
	report := ReviewReport{
		Model:           "small",
		HypothesisModel: "big",
		ParentUsage:     fantasy.Usage{InputTokens: 1_000_000, CacheReadTokens: 2_000_000},
		RulesUsage:      fantasy.Usage{InputTokens: 1_000_000, CacheReadTokens: 8_000_000, OutputTokens: 100_000},
		Correctness: CorrectnessReport{
			GeneratorUsage: fantasy.Usage{InputTokens: 1_000_000, CacheReadTokens: 0, OutputTokens: 100_000},
		},
	}
	prices := map[string]ModelTokenPrice{
		"small": {Input: 0.10, CachedInput: 0.01, Output: 0.50},
		"big":   {Input: 2.00, CachedInput: 0.10, Output: 10.00},
	}
	estimate := EstimateReviewCost(report, prices)
	// parent: 1M*0.10 + 2M*0.01            = 0.10 + 0.02        = 0.12
	// rules:  1M*0.10 + 8M*0.01 + 0.1M*0.50 = 0.10 + 0.08 + 0.05 = 0.23
	// big:    1M*2.00 + 0 + 0.1M*10.00      = 2.00 + 1.00        = 3.00
	assertCloseUSD(t, "parent", estimate.PerPartUSD[ReviewPartParent], 0.12)
	assertCloseUSD(t, "rules", estimate.PerPartUSD[ReviewPartRules], 0.23)
	assertCloseUSD(t, "correctness", estimate.PerPartUSD[ReviewPartCorrectness], 3.00)
	assertCloseUSD(t, "small", estimate.PerModelUSD["small"], 0.35)
	assertCloseUSD(t, "big", estimate.PerModelUSD["big"], 3.00)
	assertCloseUSD(t, "total", estimate.TotalUSD, 3.35)
	if len(estimate.Parts) != 3 {
		t.Fatalf("parts = %+v, want parent, rules, correctness", estimate.Parts)
	}

	t.Run("unpriced-model-is-listed", func(t *testing.T) {
		unpriced := EstimateReviewCost(ReviewReport{
			Model:       "unknown",
			ParentUsage: fantasy.Usage{InputTokens: 1},
			RulesUsage:  fantasy.Usage{InputTokens: 1},
		}, prices)
		if len(unpriced.UnpricedModels) != 1 || unpriced.TotalUSD != 0 || unpriced.Parts[0].Priced {
			t.Fatalf("unpriced estimate = %+v", unpriced)
		}
	})
}

func assertCloseUSD(t testing.TB, name string, got float64, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s cost = %.6f, want %.6f", name, got, want)
	}
}

func TestEstimateReviewCostPricesChecksAtCheckModel(t *testing.T) {
	report := ReviewReport{
		Model:                "small",
		HypothesisModel:      "big",
		HypothesisCheckModel: "mid",
		RulesUsage:           fantasy.Usage{InputTokens: 1_000_000},
		Correctness: CorrectnessReport{
			GeneratorUsage: fantasy.Usage{InputTokens: 1_000_000},
			CheckUsage:     fantasy.Usage{InputTokens: 1_000_000},
		},
	}
	prices := map[string]ModelTokenPrice{
		"small": {Input: 1},
		"mid":   {Input: 2},
		"big":   {Input: 3},
	}
	estimate := EstimateReviewCost(report, prices)
	assertCloseUSD(t, "small", estimate.PerModelUSD["small"], 1)
	assertCloseUSD(t, "mid", estimate.PerModelUSD["mid"], 2)
	assertCloseUSD(t, "big", estimate.PerModelUSD["big"], 3)
	assertCloseUSD(t, "correctness", estimate.PerPartUSD[ReviewPartCorrectness], 5)
}

func TestEstimateReviewCostMergesOnePartOnOneModel(t *testing.T) {
	report := ReviewReport{
		Model:           "small",
		HypothesisModel: "small",
		Correctness: CorrectnessReport{
			GeneratorUsage: fantasy.Usage{InputTokens: 1_000_000},
			CheckUsage:     fantasy.Usage{InputTokens: 2_000_000},
		},
	}
	estimate := EstimateReviewCost(report, map[string]ModelTokenPrice{"small": {Input: 1}})
	if len(estimate.Parts) != 1 || estimate.Parts[0].Part != ReviewPartCorrectness || estimate.Parts[0].Usage.InputTokens != 3_000_000 {
		t.Fatalf("parts = %+v, want one correctness row with 3M input tokens", estimate.Parts)
	}
	assertCloseUSD(t, "total", estimate.TotalUSD, 3)
}

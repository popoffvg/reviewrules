package review

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/popoffvg/reviewrules/internal/prompts"
	"github.com/popoffvg/reviewrules/internal/rulecatalog"
	"github.com/popoffvg/reviewrules/internal/ruleset"
)

// ReviewRunConfig is the static setup of one review run.
type ReviewRunConfig struct {
	RepoRoot        string
	Base            string
	IncludeWorktree bool
	MaxDiffBytes    int
	ModelName       string
	// Profiles replace the model of an action when their If holds for the diff; see chooseActions.
	Profiles            []ReviewProfile
	HypothesisModelName string
	// HypothesisCheckModelName answers each hypothesis; empty uses ModelName.
	HypothesisCheckModelName string
	ReasoningEffort          string
	// RuleReasoningEffort overrides ReasoningEffort for the rule forks; empty keeps ReasoningEffort.
	RuleReasoningEffort string
	Parallel            int
	ParentMaxSteps      int
	ForkMaxSteps        int
	CorrectnessEnabled  bool
	// RuleFolders are the rule folder names that the repo settings add to .reviewrules.
	RuleFolders []string
	// RuleSets are the fetched rule sets, in the order of the rule_sets list.
	RuleSets []ruleset.RuleSetCheckout
	// Prompts is the text of this run. An empty set uses the built-in prompts.
	Prompts prompts.Set
}

// RunReview loads the rules and the diff, runs the parent session once, then runs every
// in-scope rule and every hypothesis as a fork of it, at most cfg.Parallel forks at a time.
// The parent session finishes before any fork starts, so the first fork already finds its prefix in the cache.
// The first matching profile of each action picks its model; with none, cfg.HypothesisModelName writes
// the hypotheses, cfg.HypothesisCheckModelName answers them, and the parent and the rule forks run on cfg.ModelName.
func RunReview(
	ctx context.Context,
	logger *slog.Logger,
	newAgent ReviewForkAgentFactory,
	cfg ReviewRunConfig,
) (ReviewReport, error) {
	report := ReviewReport{Base: cfg.Base, Model: cfg.ModelName}

	catalog, err := rulecatalog.LoadReviewRules(ctx, cfg.RepoRoot, cfg.RuleFolders, cfg.RuleSets)
	if err != nil {
		return report, err
	}
	report.RuleDirs = catalog.RuleDirs
	logger.Info("review rules loaded", "rule_dirs", catalog.RuleDirs, "rule_sets", len(cfg.RuleSets), "rules", len(catalog.Rules))
	diff, err := LoadReviewDiff(
		ctx,
		cfg.RepoRoot,
		cfg.Base,
		cfg.IncludeWorktree,
		catalog.IgnorePatterns,
		cfg.MaxDiffBytes,
	)
	if err != nil {
		return report, err
	}
	report.ChangedFiles = diff.ChangedFiles
	if len(diff.ChangedFiles) == 0 {
		return report, fmt.Errorf("review diff is empty between %s and HEAD", cfg.Base)
	}
	choices, err := cfg.chooseActions(ProfileDiff{DiffBytes: len(diff.Patch), ChangedFiles: len(diff.ChangedFiles)})
	if err != nil {
		return report, err
	}
	report.Model = choices.Rules.Model
	report.HypothesisModel = choices.Hypothesis.Model
	report.HypothesisCheckModel = choices.HypothesisCheck.Model

	var rules []rulecatalog.ReviewRule
	for _, rule := range catalog.Rules {
		if rulecatalog.ScopeMatchesAnyFile(rule.Scope, diff.ChangedFiles) {
			rules = append(rules, rulecatalog.SplitRuleRequirements(rule)...)
		} else {
			report.SkippedRuleIDs = append(report.SkippedRuleIDs, rule.ID)
		}
	}
	docs, err := loadScopedContextDocs(cfg.RepoRoot, catalog.Docs, diff.ChangedFiles)
	if err != nil {
		return report, err
	}

	cacheKey := ReviewPromptCacheKey(diff)
	agent := newAgent(cacheKey)
	parentSettings := ForkSettings{Model: choices.Rules.Model, ReasoningEffort: cfg.ReasoningEffort, MaxSteps: cfg.ParentMaxSteps}
	ruleSettings, generatorSettings, checkSettings := choices.Rules, choices.Hypothesis, choices.HypothesisCheck
	ruleSettings.MaxSteps, generatorSettings.MaxSteps, checkSettings.MaxSteps = cfg.ForkMaxSteps, cfg.ForkMaxSteps, cfg.ForkMaxSteps

	logger.Info(
		"review parent session started",
		"changed_files", len(diff.ChangedFiles),
		"diff_bytes", len(diff.Patch),
		"model", report.Model,
		"prompt_cache_key", cacheKey,
		"reasoning_effort", cfg.ReasoningEffort,
	)
	text := cfg.Prompts
	if text.ReviewSystem == "" {
		text = prompts.Default()
	}
	parent, err := agent.StartParentSession(ctx, text.ParentSession(prompts.ParentSessionInput{
		Instructions:        catalog.Instructions,
		RuleSetInstructions: ruleSetInstructions(catalog.RuleSetInstructions),
		Docs:                docs,
		Base:                diff.Base,
		ChangedFiles:        diff.ChangedFiles,
		Patch:               diff.Patch,
	}), parentSettings)
	if err != nil {
		return report, err
	}
	report.ParentUsage, report.ParentRequests = parent.Usage, parent.Requests
	logger.Info("review parent session done", "usage", parent.Usage.String())

	limit := make(chan struct{}, max(cfg.Parallel, 1))
	var wait sync.WaitGroup
	var lock sync.Mutex
	report.Rules = make([]RuleCheckResult, len(rules))
	for index, rule := range rules {
		wait.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			check := CheckRuleInFork(ctx, agent, ruleSettings, rule, text)
			logger.Info("review rule fork done", "rule", rule.ID, "verdict", check.Verdict, "usage", check.Usage.String())
			report.Rules[index] = check
		})
	}

	report.Correctness.Enabled = cfg.CorrectnessEnabled
	if cfg.CorrectnessEnabled {
		wait.Go(func() {
			limit <- struct{}{}
			hypotheses, spent, err := GenerateHypothesesInFork(ctx, agent, generatorSettings, text)
			<-limit
			lock.Lock()
			report.Correctness.GeneratorUsage, report.Correctness.GeneratorRequests = spent.Usage, spent.Requests
			if err != nil {
				report.Correctness.GeneratorError = err.Error()
			}
			lock.Unlock()
			logger.Info("review hypothesis generator done", "hypotheses", len(hypotheses), "usage", spent.Usage.String())

			var checks []HypothesisCheck
			if err == nil {
				limit <- struct{}{}
				checks = CheckHypothesesInFork(ctx, agent, checkSettings, hypotheses, text)
				<-limit
				logger.Info("review hypothesis ledger done", "hypotheses", len(checks))
			}
			lock.Lock()
			report.Correctness.HypothesisCheck = checks
			lock.Unlock()
		})
	}
	wait.Wait()

	for _, rule := range report.Rules {
		report.RulesUsage = AddUsage(report.RulesUsage, rule.Usage)
	}
	for _, check := range report.Correctness.HypothesisCheck {
		report.Correctness.CheckUsage = AddUsage(report.Correctness.CheckUsage, check.Usage)
	}
	report.Correctness.Usage = AddUsage(report.Correctness.GeneratorUsage, report.Correctness.CheckUsage)
	report.TotalUsage = AddUsage(AddUsage(parent.Usage, report.RulesUsage), report.Correctness.Usage)
	return report, nil
}

func ruleSetInstructions(sets []rulecatalog.RuleSetInstructions) []prompts.RuleSetInstructions {
	var out []prompts.RuleSetInstructions
	for _, set := range sets {
		out = append(out, prompts.RuleSetInstructions{Name: set.RuleSet, Text: set.Text})
	}
	return out
}

// loadScopedContextDocs reads each files.json doc whose scope matches a changed file. A rule set
// doc is read inside its checkout only, and its prompt path names the rule set, because the agents'
// read tools see only the reviewed repo.
func loadScopedContextDocs(
	repoRoot string,
	docs []rulecatalog.ContextDoc,
	changedFiles []string,
) ([]prompts.ConventionDoc, error) {
	var texts []prompts.ConventionDoc
	for _, doc := range docs {
		if !rulecatalog.ScopeMatchesAnyFile(doc.Scope, changedFiles) {
			continue
		}
		path := doc.Path
		var raw []byte
		var err error
		if doc.RuleSet == "" {
			raw, err = os.ReadFile(filepath.Join(repoRoot, doc.Path)) //nolint:gosec // a doc the repo's own rule folder files.json names
		} else {
			path = fmt.Sprintf("%s (rule set %s)", doc.Path, doc.RuleSet)
			raw, err = readRuleSetDoc(doc.Root, doc.Path)
		}
		if err != nil {
			return nil, fmt.Errorf("context doc %s read failed: %w", path, err)
		}
		texts = append(texts, prompts.ConventionDoc{Path: path, Description: doc.Description, Content: string(raw)})
	}
	return texts, nil
}

// readRuleSetDoc refuses a path that leaves the checkout, so a rule set cannot make the prompt quote a file of the machine.
func readRuleSetDoc(checkoutDir string, path string) ([]byte, error) {
	root, err := os.OpenRoot(checkoutDir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	return root.ReadFile(filepath.FromSlash(path))
}

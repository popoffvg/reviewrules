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
	RepoRoot            string
	Base                string
	IncludeWorktree     bool
	MaxDiffBytes        int
	ModelName           string
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
// cfg.HypothesisModelName writes the hypotheses, cfg.HypothesisCheckModelName answers them, and
// the parent session and the rule forks run on cfg.ModelName.
func RunReview(
	ctx context.Context,
	logger *slog.Logger,
	newAgent ReviewForkAgentFactory,
	cfg ReviewRunConfig,
) (ReviewReport, error) {
	hypothesisModel := cfg.HypothesisModelName
	if hypothesisModel == "" {
		hypothesisModel = cfg.ModelName
	}
	checkModel := cfg.HypothesisCheckModelName
	if checkModel == "" {
		checkModel = cfg.ModelName
	}
	report := ReviewReport{
		Base:                 cfg.Base,
		Model:                cfg.ModelName,
		HypothesisModel:      hypothesisModel,
		HypothesisCheckModel: checkModel,
	}

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
	parentSettings := ForkSettings{Model: cfg.ModelName, ReasoningEffort: cfg.ReasoningEffort, MaxSteps: cfg.ParentMaxSteps}
	forkSettings := ForkSettings{Model: cfg.ModelName, ReasoningEffort: cfg.ReasoningEffort, MaxSteps: cfg.ForkMaxSteps}
	ruleSettings := forkSettings
	if cfg.RuleReasoningEffort != "" {
		ruleSettings.ReasoningEffort = cfg.RuleReasoningEffort
	}
	generatorSettings := forkSettings
	generatorSettings.Model = hypothesisModel
	checkSettings := forkSettings
	checkSettings.Model = checkModel

	logger.Info(
		"review parent session started",
		"changed_files", len(diff.ChangedFiles),
		"diff_bytes", len(diff.Patch),
		"prompt_cache_key", cacheKey,
		"reasoning_effort", cfg.ReasoningEffort,
	)
	text := cfg.Prompts
	if text.ReviewSystem == "" {
		text = prompts.Default()
	}
	parentUsage, err := agent.StartParentSession(ctx, text.ParentSession(prompts.ParentSessionInput{
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
	report.ParentUsage = parentUsage
	logger.Info("review parent session done", "usage", parentUsage.String())

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
			hypotheses, usage, err := GenerateHypothesesInFork(ctx, agent, generatorSettings, text)
			<-limit
			lock.Lock()
			report.Correctness.GeneratorUsage = usage
			if err != nil {
				report.Correctness.GeneratorError = err.Error()
			}
			lock.Unlock()
			logger.Info("review hypothesis generator done", "hypotheses", len(hypotheses), "usage", usage.String())

			checks := make([]HypothesisCheck, len(hypotheses))
			var checkWait sync.WaitGroup
			for index, hypothesis := range hypotheses {
				checkWait.Go(func() {
					limit <- struct{}{}
					defer func() { <-limit }()
					checks[index] = CheckHypothesisInFork(ctx, agent, checkSettings, hypothesis, text)
					logger.Info("review hypothesis fork done", "id", hypothesis.ID, "verdict", checks[index].Verdict)
				})
			}
			checkWait.Wait()
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
	report.TotalUsage = AddUsage(AddUsage(parentUsage, report.RulesUsage), report.Correctness.Usage)
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

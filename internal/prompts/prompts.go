// Package prompts holds every instruction text the review sends to an LLM, one embedded file per text.
package prompts

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed *.md *.md.tmpl
var promptFiles embed.FS

// ReviewSystem is the system prompt of every agent and every call; any change to it per fork breaks cache reuse.
// It names no tool: every agent sends this same text, and each backend has its own read and
// search tools (read_file/grep/list_dir, Read/Grep/Glob, or a read-only shell).
var ReviewSystem = promptText("review_system.md")

// HypothesisGenerator asks a fork to write correctness hypotheses, not to answer them.
var HypothesisGenerator = promptText("hypothesis_generator.md")

// JSONOnlyRetry asks a fork again for the JSON block its last reply did not have.
var JSONOnlyRetry = promptText("json_only_retry.md")

// LastStepAnswer makes a fork answer on its last step instead of reading more.
var LastStepAnswer = promptText("last_step_answer.md")

var (
	parentSessionTemplate   = promptTemplate("parent_session.md.tmpl")
	ruleCheckTemplate       = promptTemplate("rule_check.md.tmpl")
	hypothesisCheckTemplate = promptTemplate("hypothesis_check.md.tmpl")
)

// ConventionDoc is one convention doc the parent prompt quotes in full.
type ConventionDoc struct {
	Path        string
	Description string
	Content     string
}

// ParentSessionInput is the repo instructions, the rule set instructions, the convention docs, and the diff of one review.
type ParentSessionInput struct {
	Instructions string
	// RuleSetInstructions go after Instructions; with none, the prompt has no rule set section.
	RuleSetInstructions []RuleSetInstructions
	Docs                []ConventionDoc
	Base                string
	ChangedFiles        []string
	Patch               string
}

// RuleSetInstructions is the instructions text of one rule set, under a header with its name.
type RuleSetInstructions struct {
	Name string
	Text string
}

// RuleCheckInput is the one rule a fork checks.
type RuleCheckInput struct {
	ID       string
	Severity string
	Text     string
}

// HypothesisCheckInput is the one hypothesis a fork answers.
type HypothesisCheckInput struct {
	ID       string
	At       string
	Question string
	Look     string
}

// Set is the prompt text of one run. Default is the embedded text. Load replaces a file that prompts_dir holds.
type Set struct {
	ReviewSystem        string
	HypothesisGenerator string
	JSONOnlyRetry       string
	LastStepAnswer      string
	parent              *template.Template
	rule                *template.Template
	hypothesis          *template.Template
}

// Default is the embedded prompt text.
func Default() Set {
	return defaultSet
}

var defaultSet = Set{
	ReviewSystem:        ReviewSystem,
	HypothesisGenerator: HypothesisGenerator,
	JSONOnlyRetry:       JSONOnlyRetry,
	LastStepAnswer:      LastStepAnswer,
	parent:              parentSessionTemplate,
	rule:                ruleCheckTemplate,
	hypothesis:          hypothesisCheckTemplate,
}

// Prompt file names a prompts_dir entry must use. A different name is an error.
const (
	FileReviewSystem        = "review_system.md"
	FileHypothesisGenerator = "hypothesis_generator.md"
	FileJSONOnlyRetry       = "json_only_retry.md"
	FileLastStepAnswer      = "last_step_answer.md"
	FileParentSession       = "parent_session.md.tmpl"
	FileRuleCheck           = "rule_check.md.tmpl"
	FileHypothesisCheck     = "hypothesis_check.md.tmpl"
)

// Load returns the embedded prompts when dir is empty. A file in dir whose name matches a built-in
// prompt replaces that prompt. Any other file, an empty file, or a template that does not parse is an error.
func Load(dir string) (Set, error) {
	set := Default()
	if dir == "" {
		return set, nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return Set{}, fmt.Errorf("prompts_dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return Set{}, fmt.Errorf("prompts_dir %q is not a directory", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Set{}, fmt.Errorf("prompts_dir %q: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			return Set{}, fmt.Errorf("prompts_dir %q has directory %q; want a prompt file", dir, name)
		}
		raw, err := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // G304: dir is the operator's prompts_dir
		if err != nil {
			return Set{}, fmt.Errorf("prompts_dir %q read %s: %w", dir, name, err)
		}
		text := strings.TrimSuffix(string(raw), "\n")
		if strings.TrimSpace(text) == "" {
			return Set{}, fmt.Errorf("prompts_dir %q file %s is empty", dir, name)
		}
		switch name {
		case FileReviewSystem:
			set.ReviewSystem = text
		case FileHypothesisGenerator:
			set.HypothesisGenerator = text
		case FileJSONOnlyRetry:
			set.JSONOnlyRetry = text
		case FileLastStepAnswer:
			set.LastStepAnswer = text
		case FileParentSession:
			set.parent, err = parsePromptTemplate(name, text)
		case FileRuleCheck:
			set.rule, err = parsePromptTemplate(name, text)
		case FileHypothesisCheck:
			set.hypothesis, err = parsePromptTemplate(name, text)
		default:
			return Set{}, fmt.Errorf("prompts_dir %q has unknown prompt file %q", dir, name)
		}
		if err != nil {
			return Set{}, fmt.Errorf("prompts_dir %q file %s: %w", dir, name, err)
		}
	}
	return set, nil
}

// ParentSession puts the docs before the diff, so the stable part of the prefix comes first.
func ParentSession(input ParentSessionInput) string {
	return Default().ParentSession(input)
}

// ParentSession renders this set's parent prompt.
func (s Set) ParentSession(input ParentSessionInput) string {
	return render(s.parent, input)
}

// RuleCheck asks a fork to check the diff against one rule.
func RuleCheck(input RuleCheckInput) string {
	return Default().RuleCheck(input)
}

// RuleCheck renders this set's rule prompt.
func (s Set) RuleCheck(input RuleCheckInput) string {
	return render(s.rule, input)
}

// HypothesisCheck asks a fork to answer one hypothesis with quoted evidence.
func HypothesisCheck(input HypothesisCheckInput) string {
	return Default().HypothesisCheck(input)
}

// HypothesisCheck renders this set's hypothesis prompt.
func (s Set) HypothesisCheck(input HypothesisCheckInput) string {
	return render(s.hypothesis, input)
}

func parsePromptTemplate(name string, text string) (*template.Template, error) {
	return template.New(name).Option("missingkey=error").Parse(text)
}

// promptText drops the final newline of the file: no prompt ends with one.
func promptText(name string) string {
	raw, err := promptFiles.ReadFile(name)
	if err != nil {
		panic("prompts: read " + name + ": " + err.Error())
	}
	return strings.TrimSuffix(string(raw), "\n")
}

func promptTemplate(name string) *template.Template {
	return template.Must(template.New(name).Option("missingkey=error").Parse(promptText(name)))
}

// render panics on error: the inputs are typed structs, so an error is a broken template file.
func render(prompt *template.Template, input any) string {
	var text strings.Builder
	err := prompt.Execute(&text, input)
	if err != nil {
		panic("prompts: render " + prompt.Name() + ": " + err.Error())
	}
	return text.String()
}

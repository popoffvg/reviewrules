// Package prompts holds every instruction text the review sends to an LLM, one embedded file per text.
package prompts

import (
	"embed"
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

// ParentSession puts the docs before the diff, so the stable part of the prefix comes first.
func ParentSession(input ParentSessionInput) string {
	return render(parentSessionTemplate, input)
}

// RuleCheck asks a fork to check the diff against one rule.
func RuleCheck(input RuleCheckInput) string {
	return render(ruleCheckTemplate, input)
}

// HypothesisCheck asks a fork to answer one hypothesis with quoted evidence.
func HypothesisCheck(input HypothesisCheckInput) string {
	return render(hypothesisCheckTemplate, input)
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

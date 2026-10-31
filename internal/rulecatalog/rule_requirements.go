package rulecatalog

import (
	"fmt"
	"strings"
)

const (
	// minListItems: two ";"-joined clauses are usually one rule in two sentences, not a list.
	minListItems = 3
	// maxListItemLen: a longer item, or one with a sentence break, carries prose that belongs to the whole rule.
	maxListItemLen = 200
)

// SplitRuleRequirements splits a rule that is a checklist into one rule per item, so each item
// gets its own review fork. A checklist reads "<lead>: item; item; item": the lead is one clause
// (no ". " in it), with at least minListItems short items after it. Each requirement keeps the
// lead, so it still names the convention doc. Only a ";" outside parentheses separates items.
// Any other rule comes back unchanged. A requirement's ID is "<rule id>#<n>", n from 1.
func SplitRuleRequirements(rule ReviewRule) []ReviewRule {
	lead, list, found := strings.Cut(rule.Text, ":")
	if !found || strings.Contains(lead, ". ") {
		return []ReviewRule{rule}
	}
	items := splitTopLevel(list, ';')
	if len(items) < minListItems {
		return []ReviewRule{rule}
	}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if len(trimmed) > maxListItemLen || strings.Contains(outsideParentheses(trimmed), ". ") {
			return []ReviewRule{rule}
		}
	}
	requirements := make([]ReviewRule, 0, len(items))
	for index, item := range items {
		text := strings.TrimSpace(lead) + ": " + strings.TrimSuffix(strings.TrimSpace(item), ".") + "."
		requirements = append(requirements, ReviewRule{
			ID:       fmt.Sprintf("%s#%d", rule.ID, index+1),
			Text:     text,
			Scope:    rule.Scope,
			Severity: rule.Severity,
		})
	}
	return requirements
}

// outsideParentheses drops parenthesized text, so an "e.g. x" inside an aside is not read as a sentence break.
func outsideParentheses(text string) string {
	var out strings.Builder
	depth := 0
	for _, char := range text {
		switch {
		case char == '(':
			depth++
		case char == ')' && depth > 0:
			depth--
		case depth == 0:
			out.WriteRune(char)
		}
	}
	return out.String()
}

func splitTopLevel(text string, separator rune) []string {
	var items []string
	depth := 0
	start := 0
	for index, char := range text {
		switch {
		case char == '(':
			depth++
		case char == ')' && depth > 0:
			depth--
		case char == separator && depth == 0:
			items = append(items, text[start:index])
			start = index + 1
		}
	}
	items = append(items, text[start:])
	var nonEmpty []string
	for _, item := range items {
		if strings.TrimSpace(item) != "" {
			nonEmpty = append(nonEmpty, item)
		}
	}
	return nonEmpty
}

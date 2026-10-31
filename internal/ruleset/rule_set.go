// Package ruleset reads the [[rule_sets]] entries of the repo settings and fetches each rule set:
// a git repo whose root .reviewrules/ adds rules to the review of many repos.
package ruleset

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// RuleSet is one [[rule_sets]] entry of the repo settings.
type RuleSet struct {
	// Name prefixes the id of every rule of the set: "<name>/<rule id>".
	Name string `toml:"name"`
	// URL is any URL that git fetch accepts: ssh, https, or file.
	URL string `toml:"url"`
	// Ref is a tag, a branch, or a full commit sha.
	Ref string `toml:"ref"`
}

// RuleSetList is the rule_sets key of the repo settings. Decoding it rejects an unknown field, a missing field, and a duplicate name.
type RuleSetList []RuleSet

var ruleSetNamePattern = regexp.MustCompile(`^[a-z0-9-]+$`)

// ruleSetField is one field of a [[rule_sets]] entry. Every field is a required string.
type ruleSetField struct {
	name  string
	check func(value string) error
	set   func(set *RuleSet, value string)
}

func ruleSetFields() []ruleSetField {
	return []ruleSetField{
		{name: "name", check: checkRuleSetName, set: func(s *RuleSet, v string) { s.Name = v }},
		{name: "url", check: checkNoLeadingDash, set: func(s *RuleSet, v string) { s.URL = v }},
		{name: "ref", check: checkNoLeadingDash, set: func(s *RuleSet, v string) { s.Ref = v }},
	}
}

func checkRuleSetName(value string) error {
	if !ruleSetNamePattern.MatchString(value) {
		return fmt.Errorf("value %q MUST match [a-z0-9-]+", value)
	}
	return nil
}

// checkNoLeadingDash keeps a value from being read as a git option.
func checkNoLeadingDash(value string) error {
	if strings.HasPrefix(value, "-") {
		return fmt.Errorf("value %q MUST NOT start with '-'", value)
	}
	return nil
}

// UnmarshalTOML reads the [[rule_sets]] array of tables. The decoder adds the file name to the error.
func (l *RuleSetList) UnmarshalTOML(data any) error {
	entries, ok := data.([]map[string]any)
	if !ok {
		return fmt.Errorf("rule set list invalid: rule_sets MUST be an array of tables [[rule_sets]]")
	}
	fields := ruleSetFields()
	firstEntry := map[string]int{}
	list := make(RuleSetList, 0, len(entries))
	for index, entry := range entries {
		number := index + 1
		name, _ := entry["name"].(string)
		var unknown []string
		for key := range entry {
			if !slices.ContainsFunc(fields, func(field ruleSetField) bool { return field.name == key }) {
				unknown = append(unknown, key)
			}
		}
		if len(unknown) > 0 {
			slices.Sort(unknown)
			return fmt.Errorf("rule set entry invalid: [[rule_sets]] entry %d (name %q): unknown field %q", number, name, unknown[0])
		}
		var set RuleSet
		for _, field := range fields {
			value, isString := entry[field.name].(string)
			if !isString || value == "" {
				return fmt.Errorf("rule set entry invalid: [[rule_sets]] entry %d (name %q): field %q missing or not a non-empty string", number, name, field.name)
			}
			err := field.check(value)
			if err != nil {
				return fmt.Errorf("rule set entry invalid: [[rule_sets]] entry %d (name %q): field %q: %w", number, name, field.name, err)
			}
			field.set(&set, value)
		}
		if first, found := firstEntry[set.Name]; found {
			return fmt.Errorf("rule set name duplicate: [[rule_sets]] entries %d and %d both have name %q", first, number, set.Name)
		}
		firstEntry[set.Name] = number
		list = append(list, set)
	}
	*l = list
	return nil
}

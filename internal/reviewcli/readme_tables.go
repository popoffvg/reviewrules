package reviewcli

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/popoffvg/reviewrules/internal/reviewsettings"
)

// GeneratedTable is one markdown table that go generate writes between two markers of a doc file.
type GeneratedTable struct {
	Path  string
	Start string
	End   string
	Table func() (string, error)
}

// GeneratedTables lists every generated table; Path is relative to the repo root.
func GeneratedTables() []GeneratedTable {
	return []GeneratedTable{
		{Path: "README.md", Start: "<!-- flags:start -->", End: "<!-- flags:end -->", Table: func() (string, error) {
			commandLine, err := NewReviewCommandLine()
			if err != nil {
				return "", err
			}
			return FlagsTable(commandLine.Parser), nil
		}},
		{Path: "docs/configuration.md", Start: "<!-- config:start -->", End: "<!-- config:end -->", Table: func() (string, error) {
			return ConfigKeysTable(reviewsettings.ConfigKeys()), nil
		}},
	}
}

var markdownEscape = strings.NewReplacer("|", `\|`, "<", "&lt;", ">", "&gt;")

// FlagsTable renders every visible option of parser as a markdown table, one row per flag in declaration order.
func FlagsTable(parser *flags.Parser) string {
	var table strings.Builder
	table.WriteString("| Flag | Default | Meaning |\n|---|---|---|\n")
	for _, group := range parser.Groups() {
		for _, option := range group.Options() {
			if option.Hidden || option.LongName == "" {
				continue
			}
			fmt.Fprintf(&table, "| `--%s` | %s | %s |\n", option.LongName, flagDefault(option), markdownEscape.Replace(option.Description))
		}
	}
	return table.String()
}

// ConfigKeysTable renders one markdown row per config key.
func ConfigKeysTable(keys []reviewsettings.ConfigKey) string {
	var table strings.Builder
	table.WriteString("| Key | File | Default | Meaning |\n|---|---|---|---|\n")
	for _, key := range keys {
		fmt.Fprintf(&table, "| `%s` | `%s` | %s | %s |\n", key.Name, key.File, codeSpan(key.Default), markdownEscape.Replace(key.Meaning))
	}
	return table.String()
}

// flagDefault is the default-mask tag, else the default tag, else the field value before parsing.
func flagDefault(option *flags.Option) string {
	text := option.DefaultMask
	if text == "" {
		text = strings.Join(option.Default, ", ")
	}
	if text == "" {
		value := reflect.ValueOf(option.Value())
		if !value.IsZero() || value.Kind() == reflect.Bool {
			text = fmt.Sprint(value.Interface())
		}
	}
	return codeSpan(text)
}

func codeSpan(text string) string {
	if text == "" {
		return ""
	}
	return "`" + text + "`"
}

// ReplaceTable puts the table between the markers of doc.
func (g GeneratedTable) ReplaceTable(doc string, table string) (string, error) {
	before, rest, found := strings.Cut(doc, g.Start)
	if !found {
		return "", errors.New("generated table: start marker " + g.Start + " is missing in " + g.Path)
	}
	_, after, found := strings.Cut(rest, g.End)
	if !found {
		return "", errors.New("generated table: end marker " + g.End + " is missing in " + g.Path)
	}
	return before + g.Start + "\n" + table + g.End + after, nil
}

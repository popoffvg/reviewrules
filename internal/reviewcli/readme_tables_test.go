package reviewcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jessevdk/go-flags"
)

func TestGeneratedTablesAreCurrent(t *testing.T) {
	for _, generated := range GeneratedTables() {
		t.Run(generated.Path, func(t *testing.T) {
			doc, err := os.ReadFile(filepath.Join("..", "..", generated.Path))
			if err != nil {
				t.Fatal(err)
			}
			table, err := generated.Table()
			if err != nil {
				t.Fatal(err)
			}
			want, err := generated.ReplaceTable(string(doc), table)
			if err != nil {
				t.Fatal(err)
			}
			if string(doc) != want {
				t.Fatalf("%s generated table is not current; run go generate ./...", generated.Path)
			}
		})
	}
}

func TestGeneratedTablesHaveEveryRow(t *testing.T) {
	tables := map[string]string{}
	for _, generated := range GeneratedTables() {
		table, err := generated.Table()
		if err != nil {
			t.Fatal(err)
		}
		tables[generated.Path] = table
	}
	for _, tc := range []struct {
		path string
		row  string
	}{
		{"README.md", "| `--worktree` | `false` |"},
		{"README.md", "| `--print-config` | `false` |"},
		{"docs/configuration.md", "| `correctness` | `.reviewrules/settings.toml` | `true` |"},
		{"docs/configuration.md", "| `parallel` | `~/.config/reviewrules/config.toml` | `8` |"},
		{"docs/configuration.md", "| `prices` | `~/.config/reviewrules/config.toml` | `built in for the default models` |"},
		{"docs/configuration.md", "| `hypothesis_check_model` | `~/.config/reviewrules/config.toml` | `model` |"},
	} {
		if !strings.Contains(tables[tc.path], tc.row) {
			t.Errorf("%s table has no row starting %q:\n%s", tc.path, tc.row, tables[tc.path])
		}
	}
	if strings.Contains(tables["README.md"], "--agent") {
		t.Errorf("flags table has the removed flag --agent:\n%s", tables["README.md"])
	}
}

func TestReviewCommandLineParse(t *testing.T) {
	commandLine, err := NewReviewCommandLine()
	if err != nil {
		t.Fatal(err)
	}
	_, err = commandLine.Parser.ParseArgs([]string{"--worktree", "--print-config"})
	if err != nil {
		t.Fatal(err)
	}
	got := *commandLine.Flags
	if !got.Worktree || !got.PrintConfig || got.Base != "origin/main" {
		t.Fatalf("flags = %+v; want worktree on, print-config on, default base", got)
	}
}

func TestReviewCommandLineRejectsRemovedFlag(t *testing.T) {
	commandLine, err := NewReviewCommandLine()
	if err != nil {
		t.Fatal(err)
	}
	commandLine.Parser.Options &^= flags.PrintErrors
	_, err = commandLine.Parser.ParseArgs([]string{"--agent", "codex"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("err = %v; want unknown flag", err)
	}
}

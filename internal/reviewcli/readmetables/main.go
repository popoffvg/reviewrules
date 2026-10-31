// Command readmetables writes the generated tables into the doc files under the repo root given as the only argument.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/popoffvg/reviewrules/internal/reviewcli"
)

func main() {
	err := writeGeneratedTables(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "readmetables:", err)
		os.Exit(1)
	}
}

func writeGeneratedTables(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("want one argument, the repo root; got %d", len(args))
	}
	for _, generated := range reviewcli.GeneratedTables() {
		path := filepath.Join(args[0], generated.Path)
		doc, err := os.ReadFile(path) //nolint:gosec // G304: the path is the go:generate argument
		if err != nil {
			return err
		}
		table, err := generated.Table()
		if err != nil {
			return err
		}
		updated, err := generated.ReplaceTable(string(doc), table)
		if err != nil {
			return err
		}
		err = os.WriteFile(path, []byte(updated), 0o600) //nolint:gosec // G703: the path is the go:generate argument
		if err != nil {
			return err
		}
	}
	return nil
}

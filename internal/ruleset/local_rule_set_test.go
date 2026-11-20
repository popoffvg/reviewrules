package ruleset

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLocalRuleSetsResolvesPaths(t *testing.T) {
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, dir := range []string{filepath.Join(repo, ".notes"), home} {
		err := os.MkdirAll(filepath.Join(dir, ".reviewrules"), 0o750)
		if err != nil {
			t.Fatal(err)
		}
		err = os.WriteFile(filepath.Join(dir, ".reviewrules", "config.json"), []byte(`{"rules":[]}`), 0o600)
		if err != nil {
			t.Fatal(err)
		}
	}
	checkouts, skipped, err := LocalRuleSets(repo, map[string]string{"adr": ".notes", "global": "~/", "absent": "missing"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, checkout := range checkouts {
		got = append(got, checkout.Name+"="+checkout.Dir)
	}
	want := []string{"adr=" + filepath.Join(repo, ".notes"), "global=" + home}
	if !slices.Equal(got, want) {
		t.Errorf("checkouts = %v; want %v", got, want)
	}
	if !slices.Equal(skipped, []string{filepath.Join(repo, "missing")}) {
		t.Errorf("skipped = %v; want the missing folder", skipped)
	}

	_, _, err = LocalRuleSets(repo, map[string]string{"Bad_Name": ".notes"})
	if err == nil || !strings.Contains(err.Error(), "rule_dirs.Bad_Name") {
		t.Fatalf("err = %v; want the bad name refused", err)
	}
}

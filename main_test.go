package main

import "testing"

func TestReviewJSONTarget(t *testing.T) {
	cases := []struct {
		name        string
		jsonOutPath string
		ciEnv       string
		want        string
	}{
		{"local-run-no-json", "", "", ""},
		{"ci-env-false-no-json", "", "false", ""},
		{"ci-true-goes-to-stdout", "", "true", jsonToStdout},
		{"flag-file-wins-over-ci", "review.json", "true", "review.json"},
		{"flag-file-local", "review.json", "", "review.json"},
		{"flag-stdout-local", jsonToStdout, "", jsonToStdout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := reviewJSONTarget(tc.jsonOutPath, tc.ciEnv)
			if got != tc.want {
				t.Fatalf("reviewJSONTarget(%q, %q) = %q, want %q", tc.jsonOutPath, tc.ciEnv, got, tc.want)
			}
		})
	}
}

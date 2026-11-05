package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"charm.land/fantasy"
)

func TestAnthropicProviderSendsTheWorkspaceID(t *testing.T) {
	headers := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers <- r.Header.Get("anthropic-workspace-id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"m",`+
			`"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	t.Cleanup(server.Close)
	t.Setenv("ANTHROPIC_WORKSPACE_ID", "wrkspc_test")

	provider, err := newAnthropicProvider(server.URL, "test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := provider.LanguageModel(t.Context(), "claude-haiku-5-5")
	if err != nil {
		t.Fatal(err)
	}
	_, err = model.Generate(t.Context(), fantasy.Call{Prompt: fantasy.Prompt{fantasy.NewUserMessage("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-headers; got != "wrkspc_test" {
		t.Errorf("anthropic-workspace-id = %q, want wrkspc_test", got)
	}
}

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

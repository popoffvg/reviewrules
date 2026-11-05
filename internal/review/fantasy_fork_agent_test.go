package review

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/anthropic"
	"charm.land/fantasy/providers/openaicompat"
	"github.com/popoffvg/reviewrules/internal/prompts"
)

const anthropicReply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5",
"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
"usage":{"input_tokens":10,"output_tokens":2,"cache_read_input_tokens":5}}`

const openAIReply = `{"id":"c1","object":"chat.completion","created":0,"model":"gpt-6-luna",
"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],
"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`

// recordedRequests answers every call with reply and keeps each request body.
func recordedRequests(t *testing.T, reply string) (*httptest.Server, func() [][]byte) {
	t.Helper()
	var mutex sync.Mutex
	var bodies [][]byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mutex.Lock()
		bodies = append(bodies, body)
		mutex.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	return server, func() [][]byte {
		mutex.Lock()
		defer mutex.Unlock()
		return bodies
	}
}

func runParentAndFork(t *testing.T, agent *FantasyForkAgent) {
	t.Helper()
	settings := ForkSettings{Model: "claude-haiku-5-5", ReasoningEffort: "high", MaxSteps: 3}
	_, err := agent.StartParentSession(t.Context(), "read the diff", settings)
	if err != nil {
		t.Fatalf("parent session: %v", err)
	}
	_, err = agent.ForkParentSession(t.Context(), "check one rule", settings)
	if err != nil {
		t.Fatalf("fork: %v", err)
	}
}

// anthropicRequest is the part of a Messages API request body that the test reads.
type anthropicRequest struct {
	MaxTokens    int `json:"max_tokens"`
	OutputConfig struct {
		Effort string `json:"effort"`
	} `json:"output_config"`
	Messages []struct {
		Content []struct {
			Text         string          `json:"text"`
			CacheControl json.RawMessage `json:"cache_control"`
		} `json:"content"`
	} `json:"messages"`
}

func TestAnthropicCallsMarkTheLastMessageForTheCache(t *testing.T) {
	server, bodies := recordedRequests(t, anthropicReply)
	provider, err := anthropic.New(anthropic.WithBaseURL(server.URL), anthropic.WithAPIKey("test"))
	if err != nil {
		t.Fatal(err)
	}
	runParentAndFork(t, NewFantasyForkAgent(provider, t.TempDir(), AnthropicCallOptions(), prompts.Default(), ""))

	requests := bodies()
	if len(requests) != 2 {
		t.Fatalf("got %d requests, want the parent and the fork", len(requests))
	}
	for index, body := range requests {
		var request anthropicRequest
		err := json.Unmarshal(body, &request)
		if err != nil {
			t.Fatalf("request %d: %v", index, err)
		}
		if request.MaxTokens != anthropicMaxOutputTokens || request.OutputConfig.Effort != "high" {
			t.Errorf("request %d: max_tokens=%d effort=%q", index, request.MaxTokens, request.OutputConfig.Effort)
		}
		lastMessage := request.Messages[len(request.Messages)-1]
		lastBlock := lastMessage.Content[len(lastMessage.Content)-1]
		if len(lastBlock.CacheControl) == 0 {
			t.Errorf("request %d: the last block %q has no cache_control", index, lastBlock.Text)
		}
		if marks := strings.Count(string(body), `"cache_control"`); marks != 1 {
			t.Errorf("request %d: %d cache_control marks, want 1", index, marks)
		}
	}
}

func TestSessionDirGetsTheParentAndEachForkAfterTheParent(t *testing.T) {
	server, _ := recordedRequests(t, anthropicReply)
	provider, err := anthropic.New(anthropic.WithBaseURL(server.URL), anthropic.WithAPIKey("test"))
	if err != nil {
		t.Fatal(err)
	}
	sessionDir := t.TempDir()
	runParentAndFork(t, NewFantasyForkAgent(provider, t.TempDir(), AnthropicCallOptions(), prompts.Default(), sessionDir))

	parent := readSession(t, filepath.Join(sessionDir, "parent.json"))
	fork := readSession(t, filepath.Join(sessionDir, "fork-1.json"))
	if len(parent) != 3 || parent[0].Role != fantasy.MessageRoleSystem || parent[1].Role != fantasy.MessageRoleUser {
		t.Errorf("parent.json: %d messages, want system, user, assistant: %+v", len(parent), parent)
	}
	if len(fork) != 2 || fork[0].Role != fantasy.MessageRoleUser || fork[1].Role != fantasy.MessageRoleAssistant {
		t.Errorf("fork-1.json: %d messages, want the fork prompt and the answer only: %+v", len(fork), fork)
	}
}

func readSession(t *testing.T, path string) []fantasy.Message {
	t.Helper()
	encoded, err := os.ReadFile(path) //nolint:gosec // G304: the path is in the test's temp folder
	if err != nil {
		t.Fatal(err)
	}
	var messages []fantasy.Message
	err = json.Unmarshal(encoded, &messages)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return messages
}

func TestOpenAICompatCallsSendTheCacheKeyAndNoCacheMark(t *testing.T) {
	server, bodies := recordedRequests(t, openAIReply)
	provider, err := openaicompat.New(openaicompat.WithBaseURL(server.URL), openaicompat.WithAPIKey("test"))
	if err != nil {
		t.Fatal(err)
	}
	runParentAndFork(t, NewFantasyForkAgent(provider, t.TempDir(), OpenAICompatCallOptions("key-1", false), prompts.Default(), ""))

	for index, body := range bodies() {
		text := string(body)
		if !strings.Contains(text, `"prompt_cache_key":"key-1"`) || strings.Contains(text, "cache_control") {
			t.Errorf("request %d: %s", index, text)
		}
	}
}

func TestWithCacheBreakpointLeavesTheInputUnchanged(t *testing.T) {
	messages := []fantasy.Message{fantasy.NewUserMessage("a"), fantasy.NewUserMessage("b")}
	marked := withCacheBreakpoint(messages)
	if anthropic.GetCacheControl(marked[1].ProviderOptions) == nil {
		t.Error("the last message has no cache mark")
	}
	if messages[1].ProviderOptions != nil || marked[0].ProviderOptions != nil {
		t.Error("only the copy of the last message may carry the mark")
	}
}

package codexplugin

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/popoffvg/reviewrules/internal/agentplugin"
)

func TestCodexUsageCountsOnlyTheOperation(t *testing.T) {
	plugin, repoRoot := codexFixture(t, map[string]string{
		"parent": codexEvents("parent", 109791, 93440, 1325, 126),
		"first-fork": codexEvents("first", 150000, 120000, 1600, 200) +
			codexEvents("first", 191477, 146432, 2030, 300),
		"append":       codexEvents("first", 192477, 147032, 2130, 320),
		"append-again": codexEvents("first", 194477, 148032, 2330, 350),
		"second-fork":  codexEvents("second", 110791, 94040, 1425, 146),
		"nested-fork":  codexEvents("nested", 194977, 148332, 2380, 360),
	})
	cases := []struct {
		prompt    string
		operation agentplugin.PluginOperation
		from      string
		session   string
		usage     agentplugin.PluginUsage
	}{
		{"parent", agentplugin.OperationStartParent, "", "parent", usage(16351, 93440, 1325, 126)},
		{"first-fork", agentplugin.OperationFork, "parent", "first", usage(28694, 52992, 705, 174)},
		{"append", agentplugin.OperationAppend, "first", "first", usage(400, 600, 100, 20)},
		{"append-again", agentplugin.OperationAppend, "first", "first", usage(1000, 1000, 200, 30)},
		{"second-fork", agentplugin.OperationFork, "parent", "second", usage(400, 600, 100, 20)},
		{"nested-fork", agentplugin.OperationFork, "first", "nested", usage(200, 300, 50, 10)},
	}
	for _, tc := range cases {
		t.Run(tc.prompt, func(t *testing.T) {
			reply, err := plugin.RunOperation(t.Context(), tc.operation, agentplugin.PluginRequest{
				RepoRoot: repoRoot, Prompt: tc.prompt, SessionID: tc.from,
			})
			require.NoError(t, err)
			assert.Equal(t, tc.session, reply.SessionID)
			assert.Equal(t, "done", reply.Text)
			assert.Equal(t, tc.usage, reply.Usage)
		})
	}
}

func TestCodexUsageParallelForks(t *testing.T) {
	const forks = 12
	fixtures := map[string]string{"parent": codexEvents("parent", 100, 80, 20, 3)}
	for i := range forks {
		id := fmt.Sprintf("fork-%d", i)
		fixtures[id] = codexEvents(id, 150, 100, 30, 5)
	}
	plugin, repoRoot := codexFixture(t, fixtures)
	_, err := plugin.RunOperation(t.Context(), agentplugin.OperationStartParent, agentplugin.PluginRequest{
		RepoRoot: repoRoot, Prompt: "parent",
	})
	require.NoError(t, err)

	type result struct {
		reply agentplugin.PluginReply
		err   error
	}
	results := make(chan result, forks)
	var workers sync.WaitGroup
	for i := range forks {
		workers.Go(func() {
			reply, runErr := plugin.RunOperation(t.Context(), agentplugin.OperationFork, agentplugin.PluginRequest{
				RepoRoot: repoRoot, Prompt: fmt.Sprintf("fork-%d", i), SessionID: "parent",
			})
			results <- result{reply, runErr}
		})
	}
	workers.Wait()
	close(results)
	for got := range results {
		require.NoError(t, got.err)
		assert.Equal(t, usage(30, 20, 10, 2), got.reply.Usage)
	}
}

func codexFixture(t *testing.T, fixtures map[string]string) (*CodexAgentPlugin, string) {
	t.Helper()
	t.Setenv("BG_HOME", t.TempDir())
	dir := t.TempDir()
	for prompt, events := range fixtures {
		require.NoError(t, os.WriteFile(filepath.Join(dir, prompt+".jsonl"), []byte(events), 0o600))
	}
	binary := filepath.Join(dir, "codex")
	script := "#!/bin/sh\nfor prompt do :; done\nexec cat \"$(dirname \"$0\")/$prompt.jsonl\"\n"
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o600))
	require.NoError(t, os.Chmod(binary, 0o700)) // #nosec G302 -- test stub must be executable.
	return NewCodexAgentPlugin(binary), dir
}

func codexEvents(session string, input, cached, output, reasoning int64) string {
	return fmt.Sprintf(`{"type":"thread.started","thread_id":%q}
{"type":"item.completed","item":{"type":"agent_message","text":"done"}}
{"type":"turn.completed","usage":{"input_tokens":%d,"cached_input_tokens":%d,"output_tokens":%d,"reasoning_output_tokens":%d}}
`, session, input, cached, output, reasoning)
}

func usage(input, cached, output, reasoning int64) agentplugin.PluginUsage {
	return agentplugin.PluginUsage{
		InputTokens: input, CachedInputTokens: cached, OutputTokens: output, ReasoningTokens: reasoning,
	}
}

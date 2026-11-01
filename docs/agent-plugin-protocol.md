# Agent plugin protocol

This document is the contract between reviewrules and an exec agent plugin. Read it to write a plugin in any language. You do not need to read the Go source. The protocol version is `1`. For an overview of the built-in and exec plugins and the codex and claude details, see [agent-plugins.md](agent-plugins.md).

An **agent plugin** runs the LLM sessions of one review on a backend that reviewrules does not call itself: a coding agent CLI, or an API that reviewrules does not know. A **session** is one conversation with the model that the backend keeps and can continue later. The plugin gets one operation per run, does it on the backend, and tells reviewrules the session id, the last text of the model, and the token usage.

The words MUST, MUST NOT, SHOULD, and MAY are used as in RFC 2119.

## Contents

1. [Lookup and start](#lookup-and-start)
2. [Run flow](#run-flow)
3. [Operations](#operations)
4. [Request](#request)
5. [Reply](#reply)
6. [Errors](#errors)
7. [Plugin rules](#plugin-rules)
8. [Usage and cost](#usage-and-cost)
9. [Test a plugin](#test-a-plugin)
10. [How the four plugins map the operations](#how-the-four-plugins-map-the-operations)

## Lookup and start

### Select the backend

The key `agent` in the user config `~/.config/reviewrules/config.toml` (see [configuration.md](configuration.md)) selects the backend. reviewrules looks at the name in this order and uses the first match:

| Order | Names | Backend |
|---|---|---|
| 1 | `fantasy`, `openai`, `openrouter` | an API backend in reviewrules. It is not a plugin. |
| 2 | `codex`, `claude` | a built-in plugin. It is compiled into reviewrules and uses this protocol in-process, with no subprocess for the protocol. |
| 3 | any other name | the exec plugin `<plugin_dir>/reviewrules-agent-<name>` |

Thus an exec plugin with a name from rows 1 or 2 is never used. Pick a different name.

### Find the exec plugin

- The file name is `reviewrules-agent-<name>`.
- The folder is `plugin_dir`. Set it with the user config key `plugin_dir`. The default is `$XDG_CONFIG_HOME/reviewrules/plugins`, or `~/.config/reviewrules/plugins` when `XDG_CONFIG_HOME` is empty. A relative folder is relative to the current directory of reviewrules.
- `<name>` MUST NOT be empty and MUST NOT contain `/` or `\`.
- The file MUST be a regular file (not a directory) with at least one `x` bit set. A symbolic link to such a file is correct.

reviewrules checks these items once, before the review starts. If one check fails, reviewrules stops with exit code 2 and one of these messages:

| Message | Cause |
|---|---|
| `agent plugin name "<name>" is not a plain name` | the name is empty or has a path separator |
| `agent "<name>" is not built in and has no plugin: ...` | no file at `<plugin_dir>/reviewrules-agent-<name>` |
| `agent plugin <path> is not an executable file` | the file is a directory or has no `x` bit |

### Start the process

For each operation, reviewrules starts one new process:

```
<absolute path of the plugin> <operation>
```

- **Argument.** The only argument is the operation: `start-parent`, `fork`, or `append`.
- **Working directory.** The repo root under review, as an absolute path. The request field `repo_root` has the same path.
- **Environment.** The plugin gets the full environment of reviewrules, with no change and no added variable.
- **stdin.** One JSON request (one line, no trailing newline), then end of file.
- **stdout.** reviewrules reads all of stdout after the process exits and decodes it as one JSON reply.
- **stderr.** reviewrules reads all of stderr. It uses the text only in the error message when the exit code is not 0.
- **Process group.** The process starts in a new process group. Child processes that the plugin starts are in the same group, unless the plugin moves them.
- **Background task.** reviewrules starts the process through the `background` launcher (`launch.Run`). The launcher records each run as a `bg` task: `bg ls` lists it and `bg logs <task id>` shows its stdout and stderr. The task logs go to `$BG_HOME`.
- **Cancel.** When the operator stops reviewrules (Ctrl-C), reviewrules sends SIGTERM to the process group of each running plugin. Then it waits for the process to exit. It does not send SIGKILL.
- **Time limit.** reviewrules sets no time limit on an operation. If the plugin needs one, the plugin MUST set it itself.

## Run flow

One review is one run of reviewrules. It calls the plugin in this order:

1. **`start-parent`, one time.** The prompt has the review instructions, the scoped docs, and the diff. reviewrules waits until this call ends before it starts any fork, so that every fork finds the parent history in the prompt cache of the backend. reviewrules keeps the `session_id` of the reply as the parent session.
2. **`fork` for each rule check, in parallel.** Each rule check is a fork of the parent session. The fork prompt has the text of one rule.
3. **`fork` for the hypothesis generator, in parallel with the rule forks.** This fork writes up to 10 yes/no questions about bugs in the diff. It runs only when the correctness check is on (the default; `correctness = false` in `.reviewrules/settings.toml` turns it off).
4. **`fork` for each hypothesis checker, in parallel.** When the generator is done, reviewrules forks the parent session again for each question. A checker is a fork of the parent, not of the generator.
5. **`append`, only when a fork reply has no JSON block.** Each fork MUST end with a JSON block. When the `text` of a fork reply has no usable JSON block, reviewrules sends one `append` to that fork. The prompt asks the model again for only the JSON block, and `max_steps` is `1`. reviewrules sends at most one `append` per fork.

At most `parallel` operations (user config key `parallel`, default 8) run at the same time. Thus the plugin can run up to `parallel` processes at the same time, on the same parent session.

In this version, the `session_id` of every `fork` request is the parent session id, and the `session_id` of every `append` request is the id of a fork. A plugin MUST still treat `session_id` as any session: the parent or a fork.

## Operations

| Operation | reviewrules sends it to | Request `session_id` | Reply `session_id` |
|---|---|---|---|
| `start-parent` | start the parent session with the parent prompt | absent | the id of the new session |
| `fork` | run a rule check or a hypothesis | the session to copy | the id of a new session that has the full history of the request `session_id`, then the new turn |
| `append` | ask a fork again for its JSON block | the session to continue | the same id as the request; `append` makes no new session |

### start-parent

The plugin makes a new session on the backend, sends `prompt` in it, and returns the id of the session.

Request (stdin of `reviewrules-agent-mycli start-parent`):

```json
{
  "protocol_version": 1,
  "prompt": "Review instructions, docs, and the diff…",
  "system_prompt": "You are a code reviewer…",
  "model": "gpt-6-luna",
  "reasoning_effort": "medium",
  "max_steps": 8,
  "repo_root": "/home/dev/src/shop",
  "prompt_cache_key": "reviewrules-3f9a1c0b7e2d4a65"
}
```

Reply (stdout):

```json
{
  "session_id": "parent-7d1e",
  "text": "The change adds a retry to the payment client…",
  "usage": {
    "input_tokens": 1200,
    "cached_input_tokens": 0,
    "output_tokens": 310,
    "cache_write_tokens": 14800
  }
}
```

reviewrules does not use the `text` of the parent session.

### fork

The plugin makes a new session that starts with the full history of the session `session_id`, then sends `prompt` in the new session. The source session MUST stay as it was, because other forks of it run at the same time.

Request (stdin of `reviewrules-agent-mycli fork`):

```json
{
  "protocol_version": 1,
  "prompt": "Check the diff against rule test-conventions#2 …",
  "system_prompt": "You are a code reviewer…",
  "model": "gpt-6-luna",
  "reasoning_effort": "high",
  "max_steps": 8,
  "repo_root": "/home/dev/src/shop",
  "prompt_cache_key": "reviewrules-3f9a1c0b7e2d4a65",
  "session_id": "parent-7d1e"
}
```

Reply (stdout):

```json
{
  "session_id": "fork-a41c",
  "text": "I read the test file.\n```json\n{\"checks\":[…],\"verdict\":\"PASS\",\"findings\":[]}\n```",
  "usage": {
    "input_tokens": 420,
    "cached_input_tokens": 16000,
    "output_tokens": 520,
    "reasoning_tokens": 180
  }
}
```

The `text` is the last text the model wrote. reviewrules looks for the JSON block in it. The plugin MUST return the text as the model wrote it, and MUST NOT parse it or change it.

### append

The plugin sends `prompt` as one more turn at the end of the session `session_id`, and returns the same id.

Request (stdin of `reviewrules-agent-mycli append`):

```json
{
  "protocol_version": 1,
  "prompt": "Your last reply had no JSON block. Reply now with only the JSON block the task asks for, built from what you found.",
  "system_prompt": "You are a code reviewer…",
  "model": "gpt-6-luna",
  "reasoning_effort": "high",
  "max_steps": 1,
  "repo_root": "/home/dev/src/shop",
  "prompt_cache_key": "reviewrules-3f9a1c0b7e2d4a65",
  "session_id": "fork-a41c"
}
```

Reply (stdout):

```json
{
  "session_id": "fork-a41c",
  "text": "```json\n{\"checks\":[…],\"verdict\":\"PASS\",\"findings\":[]}\n```",
  "usage": {
    "input_tokens": 60,
    "cached_input_tokens": 17200,
    "output_tokens": 140
  }
}
```

## Request

The request is one JSON object. Every field is in every request, except `session_id`, which is absent in a `start-parent` request.

| Field | Type | Operations | Meaning |
|---|---|---|---|
| `protocol_version` | integer | all | The protocol version. It is `1`. The plugin MUST exit with a non-zero code when the value is not a version it knows. |
| `prompt` | string | all | The text of the new user turn. |
| `system_prompt` | string | all | The system prompt. It is the same text in every request of a run. Use it as the system prompt of the backend, or in the nearest slot the backend has. |
| `model` | string | all | The model name. An empty string means the default model of the plugin. reviewrules sends a name in most runs (its default is `gpt-6-luna`), so a plugin for a backend with other model names SHOULD tell the operator to set `model` in the user config. |
| `reasoning_effort` | string | all | The reasoning effort, for example `low`, `medium`, `high`. An empty string means the default of the plugin. reviewrules sends `medium` by default, and `high` for the rule forks (user config keys `reasoning_effort`, `rule_reasoning_effort`). |
| `max_steps` | integer | all | The maximum number of tool-call steps for this run. `0` means no limit. The defaults are `parent_steps = 8` for `start-parent` and `fork_steps = 8` for `fork`. `append` always gets `1`. |
| `repo_root` | string | all | The absolute path of the repo under review. It is also the working directory of the process. |
| `prompt_cache_key` | string | all | A key for the prompt cache: `reviewrules-` and 16 hex characters from a hash of the diff. It is the same in every request of a run. A backend that routes its cache by a key SHOULD send it. A plugin MAY also use it to name its session storage. |
| `session_id` | string | `fork`, `append` | `fork`: the session to copy. `append`: the session to continue. Absent in `start-parent`. |

Example of a version check in bash:

```bash
request="$(cat)"
if [[ "$(jq -r '.protocol_version' <<<"$request")" != "1" ]]; then
	echo "protocol_version $(jq -r '.protocol_version' <<<"$request") is not supported" >&2
	exit 2
fi
```

## Reply

The reply is one JSON object on stdout. reviewrules ignores a field that is not in this table.

| Field | Type | Required | Meaning |
|---|---|---|---|
| `session_id` | string | yes, not empty, in every operation | `start-parent`: the new parent session. `fork`: the new fork session. `append`: the same id as the request `session_id`. |
| `text` | string | no (empty when absent) | The last text the model wrote in this run. |
| `usage` | object | no (all zero when absent) | The tokens of this run. See [Usage and cost](#usage-and-cost). |
| `usage.input_tokens` | integer | no | Input tokens that did not come from the cache. Do not count `cached_input_tokens` in this number. |
| `usage.cached_input_tokens` | integer | no | Input tokens that came from the prompt cache. |
| `usage.output_tokens` | integer | no | Output tokens, with the reasoning tokens included. |
| `usage.cache_write_tokens` | integer | no | Input tokens that were written to the cache. |
| `usage.reasoning_tokens` | integer | no | The reasoning tokens. They are a part of `output_tokens`. |

When the model makes more than one call in one run (for example one call per tool step), the usage MUST be the sum over all calls of the run.

## Errors

A plugin reports a failure with a non-zero exit code and a short reason on stderr. reviewrules does not read the reply when the exit code is not 0.

| What happens | What reviewrules does |
|---|---|
| The exit code is not 0 | The operation fails with `agent plugin <path> <operation> exited with <code>: <stderr text, trimmed>`. Each exit code that is not 0 has the same effect. |
| stdout is not one JSON object | The operation fails with `agent plugin <path> <operation> reply is not JSON: ...`. A log line on stdout causes this. |
| The reply has no `session_id`, or it is empty | The operation fails with `agent plugin <path> <operation> reply has no session_id`. |
| The process cannot start | The operation fails with `agent plugin <path> <operation> failed: ...`. |
| The operator stops reviewrules | reviewrules sends SIGTERM to the process group, waits for the exit, and the operation fails with `context canceled`. |
| The `text` is empty, or has no usable JSON block | This is not a plugin error. After a `fork`, reviewrules sends one `append`. If the `append` reply also has no JSON block, the check has no answer. |

The effect of a failed operation on the review:

| Failed operation | Effect |
|---|---|
| `start-parent` | The review stops. reviewrules exits with code 2. |
| `fork` or `append` of a rule check | The rule gets the verdict `ERROR` with the error text. This is a warning, not a failure, unless the repo settings set `fail_on_unanswered = true`. |
| `fork` or `append` of the hypothesis generator | The report shows `hypothesis generator gave no answer`. This is a warning, unless the repo settings set `fail_on_unanswered = true`. |
| `fork` or `append` of a hypothesis checker | The hypothesis gets the verdict `UNSURE` with the error text. A failed check is never a defect. |

reviewrules does not run a failed operation again. When the error is temporary (for example a rate limit), the plugin MAY try again itself before it exits.

## Plugin rules

- **Read-only.** A plugin MUST NOT change the repo under review. It MUST give the model only read and search tools (for example read, grep, glob, ls), or a read-only sandbox. A tool that writes a file or runs a shell command with write access MUST NOT be available.
- **No agent config of the reviewed repo.** A plugin MUST NOT load the agent config of the repo under review: no `AGENTS.md`, `CLAUDE.md`, project settings, project skills, hooks, or MCP servers from that repo. The review rules come from the rule folders, and this config changes the prompt of each fork and breaks the cache. A plugin SHOULD also not load the user's own agent settings, for the same reason.
- **System prompt.** A plugin MUST use `system_prompt` as the system prompt, unchanged. It MUST NOT change it per operation, because a change breaks the cache.
- **fork.** `fork` MUST make a new session that starts with the full history of the request `session_id`. It MUST leave the source session as it is, because more forks of the same session run at the same time. It SHOULD copy the history and not send it to the model again, so that the backend serves the history from its prompt cache.
- **append.** `append` MUST add one turn to the end of the session that `session_id` names. It MUST NOT make a new session, and the reply `session_id` MUST be the request `session_id`.
- **One JSON object on stdout.** A plugin MUST write only the reply JSON to stdout, and nothing else. Logs, progress, and the output of the backend CLI go to stderr or to a file.
- **Exit code.** A plugin MUST exit with 0 only when the reply is complete. It MUST exit with a non-zero code on each failure: an unknown operation, an unknown `protocol_version`, a missing `session_id` in `fork` or `append`, a backend error.
- **max_steps.** When the backend has a step limit (max turns, max steps), a plugin MUST set it to `max_steps` when `max_steps` is greater than 0. When the backend has no step limit, the plugin MUST ignore `max_steps` and say so in its own docs. Then a fork can use more tool calls than the operator set.
- **reasoning_effort.** When the backend has a reasoning effort setting, a plugin MUST pass `reasoning_effort` to it when the value is not empty. When the backend has no such setting, the plugin MUST ignore the field and say so in its own docs. When the backend uses other level names, the plugin SHOULD map `low`, `medium`, `high` to the nearest level.
- **Parallel runs.** Many runs of the plugin happen at the same time. A plugin MUST NOT keep state between runs, except the sessions that the backend keeps. Temporary files MUST have a unique name per run.
- **SIGTERM.** A plugin SHOULD stop its backend process and exit soon after SIGTERM, because reviewrules waits for the exit.

## Usage and cost

reviewrules adds the `usage` of each reply to one of three parts of the review. The summary prints one `usage:` line for each part and model, and the JSON report has the same numbers.

| Part | Operations in the part | JSON report fields |
|---|---|---|
| `parent` | `start-parent` | `parent_usage` |
| `rules` | the `fork` and the `append` of each rule check | `rules_usage`, and `usage` of each item in `rules` |
| `correctness` | the `fork` and the `append` of the hypothesis generator and of each hypothesis checker | `correctness.generator_usage`, `correctness.check_usage`, `correctness.usage` |

`total_usage` is the sum of the three parts. In the report, the reply fields have these names:

| Reply field | Report field |
|---|---|
| `input_tokens` | `input_tokens` |
| `cached_input_tokens` | `cache_read_tokens` |
| `output_tokens` | `output_tokens` |
| `cache_write_tokens` | `cache_creation_tokens` |
| `reasoning_tokens` | `reasoning_tokens` |

The cost estimate (`cost_estimate` in the report) uses the price of the model name in the settings: `model` for `parent` and `rules`, `hypothesis_model` for the generator, and `hypothesis_check_model` (or `model`) for the checkers. It is:

```
input_tokens × input price + cached_input_tokens × cached price + output_tokens × output price
```

The prices are per 1M tokens. The estimate does not price `cache_write_tokens`, and it does not add `reasoning_tokens` to `output_tokens`. Thus `output_tokens` MUST include the reasoning tokens. A model with no known price adds nothing to the total, and the summary asks the operator to add a `prices."<model>"` table with `input`, `cached`, and `output` to the user config.

When an operation fails (a non-zero exit, a reply that is not JSON), reviewrules counts zero tokens for it.

## Test a plugin

### By hand

Send one request to the plugin. The reply MUST be one JSON object with a `session_id`:

```bash
plugin=~/.config/reviewrules/plugins/reviewrules-agent-mycli
echo '{"protocol_version":1,"prompt":"Say OK.","system_prompt":"Be short.","repo_root":"'"$PWD"'","max_steps":1}' \
	| "$plugin" start-parent
```

Put the `session_id` of the reply in the request field `session_id` and run `"$plugin" fork`. Then make sure that a bad version fails: `echo '{"protocol_version":2}' | "$plugin" start-parent; echo "exit $?"` MUST print an exit code that is not 0.

### With a stub CLI and a Go test

The plugins in this repo have a Go test that runs the plugin through `agentplugin.ExecAgentPlugin`, the same code that reviewrules uses. The test puts a stub of the backend CLI first on `PATH`, so it needs no model and no login. Use `plugins/pi/` and `plugins/opencode/` as the worked examples.

| Part | `plugins/pi/` | `plugins/opencode/` |
|---|---|---|
| Plugin | `reviewrules-agent-pi` | `reviewrules-agent-opencode` |
| Stub CLI | `testdata/pi`: writes its args to `$PI_STUB_ARGS_FILE` and prints a fixed JSON event stream; `PI_STUB_STOP_REASON` sets the stop reason of the last message | `testdata/opencode`: writes its args, config, and system prompt to `$OPENCODE_STUB_*_FILE`; `OPENCODE_STUB_ERROR` makes `run` fail |
| Test file | `pi_agent_plugin_test.go` | `opencode_agent_plugin_test.go` |
| Live test | `REVIEWRULES_PI_LIVE=1 go test ./plugins/pi/`; `REVIEWRULES_PI_MODEL` selects the model | `REVIEWRULES_OPENCODE_LIVE=1 go test ./plugins/opencode/`; `REVIEWRULES_OPENCODE_MODEL` selects the model |

The pattern of the test:

1. Build the plugin with `agentplugin.NewExecAgentPlugin(<absolute path of the script>)`.
2. Put the `testdata` folder first on `PATH` with `t.Setenv`. Set `BG_HOME` to `t.TempDir()`, so that the launcher writes its task logs there.
3. For each operation, call `plugin.RunOperation(t.Context(), operation, request)`. Then read the args that the stub wrote, and check the flags: the read-only tools, the system prompt, the model, the effort, and the session flags of the operation.
4. Check the reply: `session_id` is the new session for `start-parent` and `fork`, and the request `session_id` for `append`; `text` is the last assistant text; `usage` is the sum of all model calls.
5. Check the failures: a backend error and `protocol_version` 2 MUST give an error that has the stderr text of the plugin.
6. Add a live test that is skipped unless an environment variable is `1`. It runs `start-parent` with a code word, a `fork` that MUST know the code word and have a new id, and an `append` that MUST keep the fork id.

A short example:

```go
func TestMyCLIAgentPluginStartParent(t *testing.T) {
	stubDir, _ := filepath.Abs("testdata")
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BG_HOME", t.TempDir())
	path, _ := filepath.Abs(agentplugin.ExecPluginPrefix + "mycli")
	plugin := agentplugin.NewExecAgentPlugin(path)

	reply, err := plugin.RunOperation(t.Context(), agentplugin.OperationStartParent, agentplugin.PluginRequest{
		ProtocolVersion: agentplugin.PluginProtocolVersion,
		Prompt:          "check the rule",
		SystemPrompt:    "you review code",
		RepoRoot:        t.TempDir(),
		PromptCacheKey:  "cache-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.SessionID == "" {
		t.Fatal("start-parent reply has no session_id")
	}
}
```

### With reviewrules

Write a user config that selects the plugin, then run a review in a repo that has a rule folder:

```toml
# mycli.toml
[local]
agent = "mycli"
plugin_dir = "/abs/path/to/my-plugins"   # only for a plugin outside the default folder
```

```bash
reviewrules --config mycli.toml --base origin/main --worktree
```

`bg ls` lists each plugin run, and `bg logs <task id>` shows its stdout and stderr. When every rule is `ERROR`, the fork does not see the parent history, so the model has no diff.

## How the four plugins map the operations

`codex` and `claude` are built-in plugins in `internal/agentplugin/codexplugin` and `internal/agentplugin/claudeplugin`. `pi` and `opencode` are exec plugins in `plugins/pi` and `plugins/opencode`.

| | codex | claude | pi | opencode |
|---|---|---|---|---|
| `start-parent` | `codex exec --json …` | `claude -p --session-id <new uuid>` | `pi -p --session-id <new uuid>` | `opencode run --session ses_<new id>` |
| `fork` | `codex exec … fork <session_id>` | `claude -p --resume <session_id> --fork-session` | `pi -p --fork <session_id> --session-id <new uuid>` | `opencode run --session <session_id> --fork` |
| `append` | `codex exec … resume <session_id>` | `claude -p --resume <session_id>` | `pi -p --session <session_id>` | `opencode run --session <session_id>` |
| Reply `session_id` from | the `thread.started` event | `session_id` of the JSON result | the `session` event | the first `sessionID` of the events |
| Read-only | `-s read-only` | `--tools Read,Grep,Glob --allowedTools Read,Grep,Glob` | `--tools read,grep,find,ls` | an agent that denies every tool except `read`, `grep`, `glob` |
| System prompt | `-c developer_instructions=…` | `--system-prompt` | `--system-prompt` | the agent prompt, read from a temporary file |
| No repo agent config | `-c project_doc_max_bytes=0` | `--setting-sources "" --strict-mcp-config` | `--no-approve --no-context-files --no-skills --no-prompt-templates` | `OPENCODE_DISABLE_PROJECT_CONFIG=1` |
| `max_steps` | not used: codex has no step limit | `--max-turns` | not used: pi has no step limit | the agent `steps` |
| `reasoning_effort` | `-c model_reasoning_effort=` | `--effort` | `--thinking` | `--model <model>#<effort>`, or the agent `variant` when `model` is empty |
| Usage | the delta of each `turn.completed` event from the thread’s prior cumulative usage; a fork or resume starts from the request `session_id` baseline; codex counts cached tokens in `input_tokens`, so the plugin reports them in `cached_input_tokens` only | `usage` of the JSON result | the sum over the assistant messages | the sum over the assistant messages after the last user message, from `opencode session export` |

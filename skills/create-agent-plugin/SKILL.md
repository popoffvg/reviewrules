---
name: create-agent-plugin
description: Write and test an exec agent plugin for reviewrules — an executable `reviewrules-agent-<name>` in any language that runs the parent session and the forks on a new LLM CLI or API. Use when asked to "create a reviewrules agent plugin", "add a backend to reviewrules", "wrap <cli> as a reviewrules agent", or to debug a plugin that `agent = "<name>"` in the user config does not find or that fails.
---

# Create a reviewrules agent plugin

An exec plugin is one executable file. reviewrules runs it once per call, with the operation as the only argument, one JSON request on stdin, and one JSON reply on stdout. The full contract (operations, request and reply fields, errors, plugin rules, usage, tests) is in `docs/agent-plugin-protocol.md`. Read it first.

## Steps

1. **Pick the name.** The file name is `reviewrules-agent-<name>`. `<name>` MUST NOT be `codex`, `claude`, `openai`, `openrouter`, or `fantasy`: those names select a built-in backend first. Done when the name has no `/` and no built-in uses it.
2. **Map the three operations to the backend.** Write one backend command for each of `start-parent`, `fork`, and `append`, as section "Operations" of the protocol doc says. The table at the end of that doc shows how codex, claude, pi, and opencode do it. Done when each operation has one command, `fork` copies the history and leaves the source session as it is, and `append` makes no new session.
3. **Make it read-only.** Give the model only read and search tools, or a read-only sandbox, in `repo_root`. Done when no tool the model gets can write a file.
4. **Write the plugin.** Start from the example below. Follow section "Plugin rules" of the protocol doc. Done when `bash -n` (or the language's compiler) passes and the file is executable.
5. **Install it.** Copy it into the plugin folder: `~/.config/reviewrules/plugins/` by default, or the folder in the user config key `plugin_dir`. Done when `ls -l` shows the `x` bit.
6. **Test it by hand, then with reviewrules.** See [Test](#test). Done when a review run ends with a verdict for each rule and the usage line is not all zero.

## Example: wrap a CLI in bash

This plugin wraps the `claude` CLI and pins a model. Change the `claude` lines to the CLI you wrap. It needs `jq`.

```bash
#!/usr/bin/env bash
# reviewrules-agent-claude-haiku: a reviewrules exec agent plugin on the claude CLI.
set -euo pipefail

request="$(cat)"
field() { jq -r --arg key "$1" '.[$key] // empty' <<<"$request"; }

if [[ "$(field protocol_version)" != "1" ]]; then
	echo "protocol_version $(field protocol_version) is not supported" >&2
	exit 2
fi

case "${1:-}" in
start-parent) session_args=(--session-id "$(uuidgen | tr 'A-Z' 'a-z')") ;;
fork) session_args=(--resume "$(field session_id)" --fork-session) ;;
append) session_args=(--resume "$(field session_id)") ;;
*)
	echo "operation ${1:-} is unknown" >&2
	exit 2
	;;
esac

model="$(field model)"
args=(-p --output-format json
	--tools Read,Grep,Glob --allowedTools Read,Grep,Glob
	--strict-mcp-config --setting-sources ""
	--system-prompt "$(field system_prompt)"
	--model "${model:-haiku}"
	"${session_args[@]}")
effort="$(field reasoning_effort)"
[[ -n "$effort" ]] && args+=(--effort "$effort")
max_steps="$(field max_steps)"
[[ -n "$max_steps" && "$max_steps" != "0" ]] && args+=(--max-turns "$max_steps")

cd "$(field repo_root)"
output="$(claude "${args[@]}" -- "$(field prompt)")"

jq '{
	session_id: .session_id,
	text: .result,
	usage: {
		input_tokens: .usage.input_tokens,
		cached_input_tokens: .usage.cache_read_input_tokens,
		output_tokens: .usage.output_tokens,
		cache_write_tokens: .usage.cache_creation_input_tokens
	}
}' <<<"$output"
```

## Test

Run one operation by hand. The reply MUST be one JSON object with a `session_id`:

```bash
plugin=~/.config/reviewrules/plugins/reviewrules-agent-claude-haiku
echo '{"protocol_version":1,"prompt":"Say OK.","system_prompt":"Be short.","repo_root":"'"$PWD"'","max_steps":1}' \
	| "$plugin" start-parent
```

Fork that session: put its `session_id` in the request `session_id` and run `"$plugin" fork`. Then check a bad request fails: `echo '{"protocol_version":2}' | "$plugin" start-parent; echo "exit $?"` MUST print a non-zero exit.

Then write a user config that selects the plugin, and run a review in a repo that has a rule folder:

```toml
# claude-haiku.toml
[local]
agent = "claude-haiku"
plugin_dir = "/abs/path/to/my-plugins"   # only for a plugin outside the default folder
```

```bash
reviewrules --config claude-haiku.toml --base origin/main --worktree
```

The keys are in `docs/configuration.md`.

`bg ls` lists each plugin run, and `bg logs <task id>` shows its stdout and stderr.

For the error messages and their causes, see sections "Lookup and start" and "Errors" of `docs/agent-plugin-protocol.md`. For a stub CLI and a Go test, see section "Test a plugin".

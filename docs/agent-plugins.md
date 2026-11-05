# Agent plugins

An agent plugin runs the parent session and the forks on a backend that is not an API backend. There are two kinds, and both use the same protocol:

- **Built-in plugins** `codex` and `claude` are compiled into reviewrules. They take the protocol request in-process and start only the `codex` or `claude` binary.
- **Exec plugins** are executables in any language (a bash script is enough), named `reviewrules-agent-<name>`, in `plugin_dir`. `plugin_dir` is a key of the user config; the default is `~/.config/reviewrules/plugins`.

reviewrules runs `reviewrules-agent-<name> start-parent|fork|append` with one JSON request on stdin and reads one JSON reply on stdout. A plugin MUST only read the repo. The full contract (operations, request and reply fields, errors, plugin rules, usage, tests) is in [agent-plugin-protocol.md](agent-plugin-protocol.md). To write a plugin, follow [coding-agent-plugin/skills/create-agent-plugin/SKILL.md](../coding-agent-plugin/skills/create-agent-plugin/SKILL.md).

## Select a plugin

The key `agent` in the user config selects the backend. There is no command-line flag for it. reviewrules looks up the name in this order:

1. an API backend: `openai`, `openrouter`, `anthropic`, or `fantasy`;
2. a built-in plugin: `codex` or `claude`;
3. the exec plugin `<plugin_dir>/reviewrules-agent-<name>`.

```toml
[local]
agent = "pi"
plugin_dir = "/home/me/.config/reviewrules/plugins"
```

Write `plugin_dir` as an absolute path. Every key is in [configuration.md](configuration.md).

This repo has two exec plugins: `pi` in `plugins/pi/` and `opencode` in `plugins/opencode/`. To install one, link its script into `plugin_dir`, for example `ln -s "$PWD/plugins/pi/reviewrules-agent-pi" ~/.config/reviewrules/plugins/`.

## Codex and Claude plugins

With `agent = "codex"` or `agent = "claude"` the built-in plugin runs the local binary with its own login. The tool passes every flag below itself; the steps in [backend setup](backends.md) are all you set up.

### Codex

| What | How | Why |
|---|---|---|
| Parent session | `codex exec --json ... "<parent prompt>"` | the thread id comes from the `thread.started` event |
| Fork | `codex exec --json ... fork <parent thread> "<rule prompt>"` | every option goes before `fork`: the subcommand rejects `-s` and `-m` |
| Read-only | `-s read-only` | the agent can run shell commands but cannot write |
| Reviewer prompt | `-c developer_instructions="<reviewer prompt>"` | codex 0.166 ignores `base_instructions` both as `-c` and in a profile file, so it keeps its own base instructions under ours |
| No AGENTS.md | `-c project_doc_max_bytes=0` | the rules come from the rule folder, not from the agent docs |
| Effort | `-c model_reasoning_effort=<level>` | |

Limits: codex keeps about 17K tokens of its own instructions and tool definitions on every call, and it has no step limit, so `fork_steps` is not used.

### Claude

| What | How | Why |
|---|---|---|
| Parent session | `claude -p --session-id <uuid> "<parent prompt>"` | |
| Fork | `claude -p --resume <parent uuid> --fork-session "<rule prompt>"` | a new session that reads the parent from the cache |
| Reviewer prompt | `--system-prompt "<reviewer prompt>"` | replaces Claude Code's default system prompt |
| No user setup | `--setting-sources ""` and `--strict-mcp-config` | skips your settings, hooks, plugins, and MCP servers |
| Read-only tools | `--tools Read,Grep,Glob` and `--allowedTools Read,Grep,Glob` | |
| Step limit, effort | `--max-turns <fork_steps>`, `--effort <level>` | |

Without the system-prompt and settings flags, one fork loaded your hooks, skills, plugins, and CLAUDE.md files: a 256K-token prefix and $0.26 for a one-word reply. With them the prefix is 3.3K tokens. Do not use `--bare` instead: it skips the keychain, so a subscription login stops working ("Not logged in"). The cost estimate leaves out Claude's cache writes.

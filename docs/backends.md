# Choose a backend

Set `agent` in the user config (see [Configuration](configuration.md)). All backends get the same reviewer prompt and the same fork plan. `codex` and `claude` are built-in agent plugins. Names outside the API and built-in backends select an exec plugin (see [agent plugins](agent-plugins.md)).

| `agent` | What it calls | You need | Models (`model` / `hypothesis_model`) |
|---|---|---|---|
| `codex` | the local `codex` binary | `codex login` (ChatGPT plan or API key) | `gpt-6-luna` / `gpt-6.1-sol` |
| `claude` | the local `claude` binary | `claude` then `/login`, or `ANTHROPIC_API_KEY` | `haiku` / `opus`, with `hypothesis_check_model = "sonnet"` |
| `openai` | `https://api.openai.com/v1` | `OPENAI_API_KEY` | `gpt-6-luna` / `gpt-6.1-sol` |
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | `openai/gpt-6-luna` / `openai/gpt-6.1-sol` |
| `fantasy` | a LiteLLM proxy at `base_url` | `LITELLM_API_KEY` | the proxy's model names |
| `<name>` | the exec plugin `reviewrules-agent-<name>` in `plugin_dir` | what the plugin needs | what the plugin accepts |

Use `fantasy` for a LiteLLM proxy. The proxy route can use a ChatGPT subscription; the built-in `codex` backend uses the local CLI login directly. With an API key, `openai` or `openrouter` needs no proxy. See [LiteLLM setup](litellm.md).

To add a different agent, see [agent plugins](agent-plugins.md).

## Local configuration examples

For Codex, sign in with `codex login` and use:

```toml
[local]
agent = "codex"
model = "gpt-6-luna"
hypothesis_model = "gpt-6.1-sol"
```

For Claude, start `claude` and use `/login`, or set `ANTHROPIC_API_KEY`. Choose Claude model names explicitly:

```toml
[local]
agent = "claude"
model = "haiku"
hypothesis_model = "opus"
hypothesis_check_model = "sonnet"
```

For an API backend, set its API key environment variable and choose model names that the provider accepts. The OpenRouter names include the provider prefix:

```toml
[local]
agent = "openrouter"
model = "openai/gpt-6-luna"
hypothesis_model = "openai/gpt-6.1-sol"
```

These are example configurations. Model availability depends on your account and provider. Local CLI backends use their own authentication; subscription runs consume the plan allowance. Review costs shown by reviewrules are token-based estimates.

## Run from Claude Code or Codex

This repo is a plugin marketplace for Claude Code and for Codex. The plugin `reviewrules` gives the agent three skills: [run-reviewrules](../coding-agent-plugin/skills/run-reviewrules/SKILL.md) installs and runs reviewrules and reads its result, [create-agent-plugin](../coding-agent-plugin/skills/create-agent-plugin/SKILL.md) writes an exec agent plugin, and [find-lintable-rules](../coding-agent-plugin/skills/find-lintable-rules/SKILL.md) says which rules a linter can check instead of an LLM fork.

Claude Code:

```bash
claude plugin marketplace add popoffvg/reviewrules
claude plugin install reviewrules@reviewrules
```

Codex:

```bash
codex plugin marketplace add popoffvg/reviewrules
codex plugin add reviewrules@reviewrules
```

From a local clone, give the clone path in place of `popoffvg/reviewrules`. Then ask the agent: "review my branch with reviewrules".

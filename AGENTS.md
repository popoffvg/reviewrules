# reviewrules

reviewrules reviews a git diff against the rules in `.reviewrules/` and in the extra rule folders that `.reviewrules/settings.toml` names. It gives each rule its own LLM fork and its own verdict, and it checks correctness hypotheses in more forks. The user docs are in `README.md`.

## Architecture

### Packages

| Package | What it owns |
|---|---|
| `main.go` | the user config case, `--print-config`, the backend choice (API backend, built-in plugin, or exec plugin), the summary and the exit code |
| `internal/reviewsettings` | the two config files: the repo settings `.reviewrules/settings.toml` (`RepoSettings`) and the user config `~/.config/reviewrules/config.toml` with its `[local]` and `[ci]` cases (`UserConfig`). `ConfigKeys()` is the key table: one row per struct field (`toml`, `description`, `default-text` tags). It drives the unknown-key and wrong-file checks, `--print-config`, and the docs Config keys table |
| `internal/reviewcli` | the command line (go-flags): the five flags and `--print-config`; the README Flags table and the `docs/configuration.md` Config keys table that `go generate` writes (`readmetables`) |
| `internal/rulecatalog` | loads the rule folders that `rule_folders` names, the scoped docs, and splits a list rule into one check per item |
| `internal/ruleset` | the `[[rule_sets]]` entries (`RuleSet`, `RuleSetList`), the git fetch and cache (`FetchRuleSets`); see docs/rule-sets.md |
| `internal/review` | the run: diff, parent session, rule forks, hypothesis forks, report, cost; the `ReviewForkAgent` interface; the fantasy agent for the API backends |
| `internal/prompts` | every prompt text, one embedded file per text (`go:embed`) |
| `internal/agentplugin` | the agent plugin protocol (`PluginRequest`, `PluginReply`), the `AgentPlugin` interface, `PluginForkAgent`, and the exec adapter `ExecAgentPlugin` |
| `internal/agentplugin/codexplugin` | the built-in `codex` plugin |
| `internal/agentplugin/claudeplugin` | the built-in `claude` plugin |
| `coding-agent-plugin` | the Claude Code and Codex plugin `reviewrules` (`.claude-plugin/plugin.json`, `.codex-plugin/plugin.json`) and its skills; the marketplaces that list it are `.claude-plugin/marketplace.json` and `.agents/plugins/marketplace.json` at the repo root. Change the `version` in both plugin manifests together |

### Run flow

`review.RunReview` runs the steps in [docs/how-a-run-works.md](docs/how-a-run-works.md). The parent session (`ReviewForkAgent.StartParentSession`) MUST finish before the first fork, so that every fork finds the prefix in the prompt cache. A fork reply without the JSON block gets one `AppendToFork` with `prompts.JSONOnlyRetry`.

## Agent plugins

The backend is `ReviewForkAgent`. The API backends `openai`, `openrouter`, and `fantasy` are in the core (`review.FantasyForkAgent`) and are not plugins. Every other agent is a plugin.

### Lookup

The user config key `agent = "<name>"` selects an API backend row in `reviewEndpoints()` (`main.go`), else a built-in plugin row in `builtinAgentPlugins()` (`main.go`): `codex`, `claude`, else the exec plugin `<plugin_dir>/reviewrules-agent-<name>` (`agentplugin.FindExecAgentPlugin`).

### Built-in and exec plugins

- A built-in plugin implements `agentplugin.AgentPlugin` in-process. It gets the same `PluginRequest` and gives the same `PluginReply` as an exec plugin, with no subprocess for the protocol.
- `agentplugin.ExecAgentPlugin` implements the same interface for an external executable in any language.
- `agentplugin.PluginForkAgent` turns any `AgentPlugin` into a `ReviewForkAgent`. It keeps the parent session id and sends it with each fork.
- Start every child process (the codex and claude CLIs, every exec plugin) through `github.com/popoffvg/background/launch.Run`, so `bg ls` shows it. A non-zero exit is not an error from `launch.Run`; the caller MUST check `ExitCode`.

### Protocol

The user doc is [docs/agent-plugins.md](docs/agent-plugins.md). The protocol and the plugin rules are in [docs/agent-plugin-protocol.md](docs/agent-plugin-protocol.md). In short: reviewrules runs `reviewrules-agent-<name> start-parent|fork|append` in the repo root, writes one `PluginRequest` JSON to stdin, and reads one `PluginReply` JSON from stdout. A non-zero exit is an error, and stderr is its message. Change the doc in the same change as `agent_plugin_protocol.go`, `exec_agent_plugin.go`, or `plugin_fork_agent.go`.

### Add a backend

- **Exec plugin** (any language, no reviewrules change): follow `coding-agent-plugin/skills/create-agent-plugin/SKILL.md`.
- **Built-in plugin**: add a package `internal/agentplugin/<name>plugin` with a type that implements `agentplugin.AgentPlugin`, add the name to `reviewsettings.ReviewAgentKind`, and add one row to `builtinAgentPlugins()` in `main.go`.
- **API backend** (OpenAI-compatible endpoint): add one row to `reviewEndpoints()` in `main.go`.

### Exec plugins in this repo

- **pi** (`plugins/pi/reviewrules-agent-pi`, bash + jq): runs the `pi` coding agent CLI with the tools `read,grep,find,ls` only. Install: copy or link the script into `plugin_dir`, e.g. `ln -s "$PWD/plugins/pi/reviewrules-agent-pi" ~/.config/reviewrules/plugins/`, then set `agent = "pi"` in the user config. Sessions go to `$PI_SESSION_DIR`, else to `$TMPDIR/reviewrules-pi/<prompt_cache_key>`. `max_steps` has no effect. `plugins/pi/testdata/pi` is the stub `pi` for the tests; `task live` runs the real `pi` (`REVIEWRULES_PI_MODEL` picks the model).
- **opencode** (`plugins/opencode/reviewrules-agent-opencode`, bash + jq): runs `opencode run --standalone` with a generated agent that allows only `read`, `grep`, and `glob`. The system prompt is the agent prompt. `reasoning_effort` is the model variant (`-m model#effort`). Install: `ln -s "$PWD/plugins/opencode/reviewrules-agent-opencode" ~/.config/reviewrules/plugins/`, then set `agent = "opencode"` and `model = "provider/model"` in the user config. `plugins/opencode/testdata/opencode` is the stub `opencode` for the tests; `task live` runs the real `opencode` (`REVIEWRULES_OPENCODE_MODEL` picks the model).

## Develop

The tools are pinned in `mise.toml`; the commands are tasks in `Taskfile.yml`.

```bash
mise install                           # Go, task, actionlint, zizmor
task setup                             # mise install and a pre-push hook that runs `task check`
task generate                          # README Flags table and docs/configuration.md Config keys table; a test fails when either is stale
task build
task test                              # go test -race
task lint                              # golangci-lint: gosec, revive, and the ruleguard rules in .ruleguard/
task workflows                         # actionlint and zizmor --offline
task check                             # all of the above, in CI order
task live                              # pi and opencode plugin tests against the real CLIs
```

- Run builds, tests, and lint in the background; they take time.
- `internal/agentplugin/testdata/reviewrules-agent-fixed` is the test exec plugin. Tests set `BG_HOME` to a temp dir, so `launch.Run` writes its task logs there.
- Code rules: comments only where the code needs clarification; a doc is at most 60 words; a file is named after its concept; a table of similar items is one declarative table plus one reader loop.

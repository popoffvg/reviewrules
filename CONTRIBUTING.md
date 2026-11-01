# Contributing to reviewrules

The tools are pinned in `mise.toml`, and the commands are tasks in `Taskfile.yml` (go-task v3).

```bash
mise install                           # Go, task, actionlint, and zizmor at the pinned versions
task setup                             # also installs a git pre-push hook that runs `task check`
task check                             # everything CI runs: generate, build, test, lint, workflows
task --list                            # every task with its description
```

- `task generate` writes the Flags table of README.md and the Config keys table of docs/configuration.md. A test fails when either table is stale.
- `golangci-lint` is pinned as a Go tool in `go.mod` (v2.14.0; older versions cannot read Go 1.27 packages). `.golangci.yaml` enables gosec and gocritic's ruleguard with the rules in `.ruleguard/reviewrules_rules.go`.
- `task live` runs the pi and opencode plugin tests against the real CLIs. `REVIEWRULES_PI_MODEL` and `REVIEWRULES_OPENCODE_MODEL` pick the models.
- CI (`.github/workflows/ci.yaml`) installs the tools from `mise.toml` and runs three jobs: `task test`, `task lint`, and `task workflows`. Actions are pinned by commit, checkout keeps no credentials, and the token is read-only.

## Before sending a change

Run builds, tests, and lint in the background. Run `task check` before pushing; it includes generated docs, build, tests with the race detector, lint, and workflow checks.

Keep user-facing instructions with the feature they describe. The [README](README.md) covers the purpose and first run. Detailed guides live in [`docs/`](docs/). Preserve the generated table markers when editing the README or configuration reference.

## Code conventions

Name files after the concept they contain. Add comments where the code needs clarification, and keep each doc comment within 60 words. Represent similar items as one declarative table and one reader loop. See [AGENTS.md](AGENTS.md) for package ownership and the run constraints.

## Extend a backend

- **Exec plugin:** follow the [create-agent-plugin guide](coding-agent-plugin/skills/create-agent-plugin/SKILL.md). The [protocol](docs/agent-plugin-protocol.md) defines its operations and read-only contract.
- **Built-in plugin:** implement `agentplugin.AgentPlugin` in `internal/agentplugin/<name>plugin`, add the name to `reviewsettings.ReviewAgentKind`, and register it in `builtinAgentPlugins()` in `main.go`.
- **API backend:** add an OpenAI-compatible endpoint row to `reviewEndpoints()` in `main.go`.

Start child processes through `github.com/popoffvg/background/launch.Run` and check `ExitCode`. Update the protocol documentation alongside changes to the protocol or its adapters.

## Coding-agent plugin

The plugin source is in `coding-agent-plugin/`. Its Claude Code and Codex manifests must have the same version. The root marketplace manifests list the plugin. Keep plugin installation and skill instructions in sync with the CLI.

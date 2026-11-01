# reviewrules

**Your team's review rules, checked one by one.**

reviewrules is an open-source CLI that reviews a git diff against rules you write in plain language. Run it before a push or make it a pull request check in CI. Each rule gets its own LLM fork and verdict, with findings tied to a file and line.

Use it for conventions that need code context: whether errors explain the failed operation, whether a write holds the required lease, or whether a change follows your package's design.

## Why use it

- **See which rules were checked.** Every rule in scope gets a separate check. A missing answer shows as a warning.
- **Look for bugs beyond the rules.** A correctness pass proposes possible bugs. Separate forks check each one against the code and report evidence.
- **Use the backend you choose.** Run through your local Codex or Claude login, call an API, or add another coding agent as a plugin.
- **Reuse context across checks.** A parent session reads the diff and scoped docs. Parallel forks share that history, allowing prompt-cache reuse. The report breaks down usage and estimated cost.
- **Keep review policy in git.** Scope rules by file, set severity thresholds, and share rule sets across repositories. Agents review with read-only access.

Compare [reviewrules with a plain Codex review](docs/comparison.md): what each checks, how to run them, and the results of a controlled example.

## Quick start

You need **Go 1.27+**, Git, and a logged-in Codex CLI for this example. Other choices are in [backend setup](docs/backends.md).

Install reviewrules and make sure Go's binary directory is on your `PATH`:

```bash
go install github.com/popoffvg/reviewrules@latest
export PATH="$(go env GOPATH)/bin:$PATH"
codex login
```

Create `~/.config/reviewrules/config.toml` with the following settings, or add them to your existing `[local]` section. Use `$XDG_CONFIG_HOME/reviewrules/config.toml` if you set `XDG_CONFIG_HOME`.

```toml
[local]
agent = "codex"
```

In the repository you want to review, create `.reviewrules/config.json`. Start with one convention your team wants checked:

```json
{
  "instructions": "Go backend.",
  "rules": [
    {
      "id": "error-context",
      "rule": "Every new error must include enough context to identify which operation failed.",
      "scope": ["**/*.go"],
      "severity": "medium"
    }
  ]
}
```

Run it on a branch with changes against `origin/main`:

```bash
reviewrules --base origin/main --worktree
```

`--worktree` includes staged and unstaged edits to tracked files. Stage new files with `git add` to include them. Replace `origin/main` with your base branch when needed.

You get a verdict per rule, findings with `file:line`, correctness results, and usage with estimated cost. Exit code **0** means pass, **1** means the review failed its policy, and **2** means the tool could not run. See [how to read the result](docs/review-results.md).

### Run it from Claude Code or Codex

Install the reviewrules plugin, then ask your agent: **"review my branch with reviewrules"**.

```bash
# Claude Code
claude plugin marketplace add popoffvg/reviewrules
claude plugin install reviewrules@reviewrules

# Codex
codex plugin marketplace add popoffvg/reviewrules
codex plugin add reviewrules@reviewrules
```

The plugin includes skills to run a review, create an agent plugin, and identify rules a linter can handle. See [plugin setup](docs/backends.md#run-from-claude-code-or-codex).

## Run a review

```bash
reviewrules --base origin/main                    # committed branch changes
reviewrules --base origin/main --worktree         # include local edits
reviewrules --base origin/main --json-out report.json
reviewrules --print-config                        # inspect settings without a review
```

The diff starts at the merge base of your base branch and `HEAD`. Run from the repository root, or pass `--repo /path/to/repo`.

Commit review policy in `.reviewrules/settings.toml`. For example, make unanswered checks fail alongside rule violations at medium severity or higher:

```toml
fail_on = "medium"
fail_on_unanswered = true
```

Correctness checks are on by default. Set `correctness = false` in that same file to run only the rules. Backend and model choices belong in your user config. See [configuration](docs/configuration.md) for every key and default.

For pull requests, set `CI=true` and configure the `[ci]` section of the user config. The report goes to stdout as JSON. The [CI guide](docs/ci.md) has a complete GitHub Actions job.

## Flags

Every other setting lives in a config file. This table is generated from the CLI.

<!-- flags:start -->
| Flag | Default | Meaning |
|---|---|---|
| `--config` | `~/.config/reviewrules/config.toml` | user config file; [local] is used, or [ci] when CI=true |
| `--repo` | `.` | repository root to review; its .reviewrules/settings.toml is the repo settings |
| `--base` | `origin/main` | base ref; the diff starts at merge-base(base, HEAD) |
| `--worktree` | `false` | review uncommitted changes too (local run before a push) |
| `--json-out` | `none` | write the JSON report to this file, or to stdout with "-"; with CI=true and no flag it goes to stdout |
| `--print-config` | `false` | print every merged config value and the file that gave it, then exit without a review |
<!-- flags:end -->

## Guides

| I want to… | Guide |
|---|---|
| Write, scope, or organize rules | [Writing rules](docs/writing-rules.md) |
| Share rules across repositories | [Rule sets](docs/rule-sets.md) |
| Choose a backend or model | [Backend setup](docs/backends.md) |
| Configure review policy | [Configuration](docs/configuration.md) |
| Add a pull request check | [CI setup](docs/ci.md) |
| Understand warnings, findings, or cost | [Review results](docs/review-results.md) |
| Understand the forks and shared context | [How a run works](docs/how-a-run-works.md) |
| Use a ChatGPT plan through LiteLLM | [Local proxy](docs/litellm.md) · [Shared CI proxy](deploy/litellm/README.md) |
| Add an agent backend | [Agent plugins](docs/agent-plugins.md) · [Protocol](docs/agent-plugin-protocol.md) |
| Develop or contribute | [Contributing](CONTRIBUTING.md) |

## License

MIT. See [LICENSE](LICENSE).

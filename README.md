# reviewrules

reviewrules is an open-source command-line tool that uses an LLM to review a git diff against the rules your team writes. You run it on your laptop before a push, or in CI on each pull request.

A code review bot that reads all the rules in one prompt often skips some of them, and you cannot see which rule it checked. reviewrules gives each rule its own check and its own verdict, so a rule that fails shows in the output with the file and the line.

Features:

- **Rules in one JSON file.** The rules are in `.reviewrules/config.json`. Each rule has a scope glob and a severity. The tool splits a rule written as `lead: a; b; c` into one check per item.
- **One fork per rule.** The tool reads the diff once in a parent session. Then it starts one fork of that session for each rule in scope, and the forks run in parallel.
- **Correctness check, on by default.** Each run also looks for bugs that no rule names. One fork writes up to 10 yes/no questions about bugs that the diff can cause, and a different fork answers each question with evidence. It adds up to 11 forks; `correctness = false` in the repo settings turns it off. The report shows its usage and cost apart from the rules.
- **Low cost from the prompt cache.** Each fork starts with the full history of the parent session, so the provider serves that history from its prompt cache.
- **Five backends and agent plugins.** Use the local `codex` or `claude` binary with your own login, the OpenAI or OpenRouter API, or a LiteLLM proxy on a ChatGPT subscription. Add any other CLI or API as an agent plugin.
- **CI mode.** With `CI=true` the tool writes a JSON report to stdout, and the exit code tells the pipeline if the review failed.
- **Read-only agents.** Every agent can only read the repository. No agent can change a file.

## Quick start

```bash
go install github.com/popoffvg/reviewrules@latest  # needs Go 1.27; puts reviewrules in $(go env GOPATH)/bin

codex login                                   # once; or pick another backend below
mkdir -p ~/.config/reviewrules
printf '[local]\nagent = "codex"\n' > ~/.config/reviewrules/config.toml
cd /path/to/repo/under/review
reviewrules --base origin/main --worktree
```

The repo under review needs a rule folder at its root: `.reviewrules/`, or a folder that `rule_folders` in `.reviewrules/settings.toml` names. See [Write the rules](#write-the-rules).

To let an agent (Claude Code, Codex) run the review, give it [skills/run-reviewrules/SKILL.md](skills/run-reviewrules/SKILL.md).

## Choose a backend

Set `agent` in the user config (see [Configuration](#configuration)). All backends get the same reviewer prompt and the same fork plan. `codex` and `claude` are built-in agent plugins; any other name selects an exec plugin (see [docs/agent-plugins.md](docs/agent-plugins.md)).

| `agent` | What it calls | You need | Models (`model` / `hypothesis_model`) |
|---|---|---|---|
| `codex` | the local `codex` binary | `codex login` (ChatGPT plan or API key) | `gpt-6-luna` / `gpt-6.1-sol` |
| `claude` | the local `claude` binary | `claude` then `/login`, or `ANTHROPIC_API_KEY` | `haiku` / `opus`, with `hypothesis_check_model = "sonnet"` |
| `openai` | `https://api.openai.com/v1` | `OPENAI_API_KEY` | `gpt-6-luna` / `gpt-6.1-sol` |
| `openrouter` | `https://openrouter.ai/api/v1` | `OPENROUTER_API_KEY` | `openai/gpt-6-luna` / `openai/gpt-6.1-sol` |
| `fantasy` | a LiteLLM proxy at `base_url` | `LITELLM_API_KEY` | the proxy's model names |
| `<name>` | the exec plugin `reviewrules-agent-<name>` in `plugin_dir` | what the plugin needs | what the plugin accepts |

Use `fantasy` only to run on a ChatGPT subscription through LiteLLM: LiteLLM is the only part that does the ChatGPT login. With an API key, `openai` or `openrouter` needs no proxy. See [LiteLLM on a ChatGPT subscription](#litellm-on-a-chatgpt-subscription).

To add a different agent, see [docs/agent-plugins.md](docs/agent-plugins.md).

## Write the rules

Put a `config.json` in `.reviewrules/` at the repo root, or in a folder that `rule_folders` names (see [Rule folders](#rule-folders)).

```json
{
  "instructions": "Go backend. Errors use the mierr package.",
  "ignorePatterns": "**/*.pb.go\n**/testdata/**",
  "rules": [
    {
      "id": "test-conventions",
      "rule": "Tests: use testing.TB; use t.Cleanup for cleanup; use t.Context(); use t.TempDir(); use '-' instead of spaces in t.Run titles",
      "scope": ["**/*_test.go"],
      "severity": "medium"
    }
  ]
}
```

- `scope` is a list of globs. A rule whose scope matches no changed file is skipped.
- `severity` is `low`, `medium`, or `high`. `fail_on` in the repo settings sets the lowest severity that fails the run.
- A rule written as `lead: a; b; c` with 3 or more short items is split into one check per item (`test-conventions#1`, `#2`, …). Each item gets its own fork.
- `files.json` lists the docs the rules refer to (`path`, `description`, `scope`). A doc whose scope matches a changed file goes into the parent session.
- A nested folder can have its own rule folder with more rules. Only the root's `instructions` and `ignorePatterns` are used.
- An untracked root rule folder also counts, so you can try rules before you commit them. A nested rule folder counts only when git tracks it.

### Rule folders

`rule_folders` in `.reviewrules/settings.toml` at the repo root adds more rule folder names:

```toml
rule_folders = [".team-rules"]
```

- reviewrules searches each name in `rule_folders` in every directory, the same way it searches `.reviewrules`: at the root and in each nested folder.
- When one directory has more than one rule folder, only one is used: `.reviewrules` first, then the names in the order of the list.
- The file is optional. A repo can have only `.reviewrules/settings.toml` and keep its rules in `.team-rules/config.json`.
- A name that is empty, `.`, or that contains `/`, `\`, or `..` is an error: each item MUST be one folder name. On an error the run stops with exit code 2.

## Run a review

Run it from the repo under review (or pass `--repo`).

```bash
reviewrules --base origin/main --worktree        # the branch plus uncommitted changes
reviewrules --base origin/main                   # the commits only, as CI sees them
reviewrules --json-out report.json               # also write the JSON report
reviewrules --config ~/claude.toml               # another user config, e.g. agent = "claude", model = "haiku"
reviewrules --print-config                       # print the merged config and the file of each value
```

A run checks every rule in scope. The backend, the models, and the review policy come from the config files (see [Configuration](#configuration)).

The output on a terminal:

- one line per rule check: `PASS`, `FAIL` with the findings (`file:line — message`), or `ERROR` when the model gave no answer;
- one line per hypothesis: `CONFIRMED`, `REFUTED`, or `UNSURE`. A CONFIRMED hypothesis shows the input that breaks the code, the evidence lines, and the fix;
- the token usage and the cost estimate: one `usage:` line for the total, then one line per part (`parent`, `rules`, `correctness`) and model, then the total cost;
- at the end, `Review failed because:` and one line per reason, when the run fails.

Exit codes: `0` pass, `1` review failed, `2` the tool could not run.

The review fails on a FAIL rule at or above `fail_on`, or on a CONFIRMED hypothesis. An ERROR rule or a hypothesis generator error is only a warning, because a cheap model sometimes skips the answer format. With `fail_on_unanswered = true` they fail the run too, under the same `fail_on` bar.

### Correctness check

The correctness check runs by default on every run. It looks for bugs that no rule names:

1. One generator fork on `hypothesis_model` writes up to 10 yes/no hypotheses. Each hypothesis is a question about one place in the diff, and a "yes" means a bug.
2. One checker fork per hypothesis, on `hypothesis_check_model`, answers it with `CONFIRMED`, `REFUTED`, or `UNSURE` and the evidence. A checker forks the parent session, not the generator.

The check adds up to 11 forks to a run, and the generator model is often the most expensive one. To turn it off, set `correctness = false` in `.reviewrules/settings.toml`.

The summary and the JSON report show the correctness usage and cost apart from the parent session and the rule forks. In the summary, each `usage: correctness <model>` line has the tokens and the cost of that model. In the JSON report, `correctness.generator_usage`, `correctness.check_usage`, and `correctness.usage` hold the tokens, `cost_estimate.per_part_usd.correctness` holds the cost, and `cost_estimate.parts` has one row per part and model. `total_usage` and `cost_estimate.total_usd` still hold the whole run.

## Run in CI

With `CI=true` the tool reads the `[ci]` case of the user config and writes the JSON report to stdout; the summary goes to stderr. The diff needs the base branch history, so fetch it fully.

```yaml
jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
    steps:
      - uses: actions/checkout@11d5960a326750d5838078e36cf38b85af677262 # v4
        with:
          fetch-depth: 0
          persist-credentials: false
      - uses: actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5
        with:
          go-version: stable
      - name: Install reviewrules
        run: go install github.com/popoffvg/reviewrules@<sha or tag>
      - name: Review
        env:
          OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
          BASE_REF: ${{ github.base_ref }}
        run: |
          cat > "$RUNNER_TEMP/reviewrules.toml" <<'TOML'
          [ci]
          agent = "openrouter"
          model = "openai/gpt-6-luna"
          hypothesis_model = "openai/gpt-6.1-sol"
          TOML
          reviewrules --config "$RUNNER_TEMP/reviewrules.toml" --base "origin/$BASE_REF" --json-out review.json
```

Replace `<sha or tag>` with a commit SHA or a tag of reviewrules, so that each run uses the same version. `actions/setup-go` puts `$(go env GOPATH)/bin` on `PATH`. Copy the action SHAs from this repo's `.github/workflows/ci.yaml`. To run on a ChatGPT subscription instead, host the LiteLLM proxy near the runners (`deploy/litellm/`) and set `agent = "fantasy"` and `base_url = "<proxy>/v1"` in the `[ci]` case, with `LITELLM_API_KEY`. The job can also pass `--config` with a user config that the repo commits.

## Configuration

The backend, the models, and the review policy are keys in two TOML files: the repo settings `.reviewrules/settings.toml` and the user config `~/.config/reviewrules/config.toml`. See [docs/configuration.md](docs/configuration.md) for every key, its file, its default, and full examples.

## Flags

reviewrules has only these flags; a removed flag gives an `unknown flag` error. A bool flag also takes `=true` or `=false`. `go generate ./...` writes this table from the command line.

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

## LiteLLM on a ChatGPT subscription

The models run on a ChatGPT plan that includes Codex. Allow device code login in ChatGPT's security settings, then sign in once:

```bash
uv tool install 'litellm[proxy]==1.106.6'
~/.local/share/uv/tools/litellm/bin/python -c \
  "from litellm.llms.chatgpt.authenticator import Authenticator; Authenticator().get_access_token()"
chmod 600 ~/.config/litellm/chatgpt/auth.json
scripts/litellm-local.sh &               # 127.0.0.1:4000, models gpt-6-luna and gpt-6.1-sol

LITELLM_API_KEY="$(cat "$TMPDIR/reviewrules-litellm.key")" reviewrules --base origin/main --worktree   # with agent = "fantasy" in [local]
```

The ChatGPT backend caches per session, so the tool sends `litellm_session_id` to the proxy; without it the cache hit rate falls to 14–30%. For a shared proxy in CI see `deploy/litellm/README.md`.


## Develop

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

## More docs

- [docs/configuration.md](docs/configuration.md): every config key, its file, its default, and full examples.
- [docs/how-a-run-works.md](docs/how-a-run-works.md): the steps of one run.
- [docs/agent-plugins.md](docs/agent-plugins.md): built-in and exec plugins, and the codex and claude details.
- [docs/agent-plugin-protocol.md](docs/agent-plugin-protocol.md): the contract between reviewrules and an exec plugin.
- [docs/rule-sets.md](docs/rule-sets.md): share one set of rules across many repos with `[[rule_sets]]` in `.reviewrules/settings.toml`.

## License

MIT. See [LICENSE](LICENSE).

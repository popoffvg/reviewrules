# Configuration

reviewrules has five flags and `--print-config` (see the Flags table in `README.md`). Every other choice is a key in one of two TOML files. Each key has one owner file.

| File | Who writes it | What it holds |
|---|---|---|
| `.reviewrules/settings.toml` at the repo root (the repo settings) | the team; commit it | the review policy: the rule folders, the rule sets, what fails the run, the correctness check, the diff size limit |
| `~/.config/reviewrules/config.toml`, or the file that `--config` names (the user config) | each operator, or the CI job | the machine choices: the backend, the models, the effort, the parallel forks, the binaries, the prices |

`$XDG_CONFIG_HOME/reviewrules/config.toml` replaces the default user config path when `XDG_CONFIG_HOME` is set. Both files are optional. A missing file or a missing key keeps the default.

## Config keys

`go generate ./...` writes this table from the Go structs `RepoSettings` and `UserConfig` (`internal/reviewsettings`).

<!-- config:start -->
| Key | File | Default | Meaning |
|---|---|---|---|
| `rule_folders` | `.reviewrules/settings.toml` | `[]` | more rule folder names, searched like .reviewrules in every directory; .reviewrules wins, then list order |
| `fail_on` | `.reviewrules/settings.toml` | `"medium"` | lowest rule severity whose FAIL fails the run: low, medium, high |
| `fail_on_unanswered` | `.reviewrules/settings.toml` | `false` | fail the run when a rule at or above fail_on, or the hypothesis generator, gives no answer |
| `correctness` | `.reviewrules/settings.toml` | `true` | run the hypothesis generator and one checker fork per hypothesis |
| `max_diff_bytes` | `.reviewrules/settings.toml` | `300000` | refuse a diff larger than this |
| `rule_sets` | `.reviewrules/settings.toml` | `[]` | shared rule sets [[rule_sets]] with name, url, ref; each adds the rules of its root .reviewrules/ as &lt;name&gt;/&lt;rule id&gt; |
| `agent` | `~/.config/reviewrules/config.toml` | `detected` | backend: fantasy (LiteLLM endpoint), openai or openrouter (direct API), claude or codex (built-in plugins), or an exec plugin name; unset picks the one backend found on this machine, and several found fail the run |
| `base_url` | `~/.config/reviewrules/config.toml` | `"http://localhost:4000/v1"` | endpoint for fantasy; for openai and openrouter only when it differs from the default LiteLLM URL |
| `model` | `~/.config/reviewrules/config.toml` | `per agent` | model for the parent session, the rule forks, and the hypothesis checkers; unset takes the agent default: gpt-6-luna for the API backends, the CLI default for claude and codex |
| `hypothesis_model` | `~/.config/reviewrules/config.toml` | `per agent` | model that writes the correctness hypotheses; unset takes the agent default: gpt-6.1-sol for the API backends, the CLI default for claude and codex |
| `hypothesis_check_model` | `~/.config/reviewrules/config.toml` | `model` | model that answers each hypothesis; empty uses model |
| `reasoning_effort` | `~/.config/reviewrules/config.toml` | `"medium"` | effort for the parent session and the hypothesis forks |
| `rule_reasoning_effort` | `~/.config/reviewrules/config.toml` | `"high"` | effort for the rule forks; at medium a small model skips checks at random |
| `profiles` | `~/.config/reviewrules/config.toml` | `{"heavy":{"fork_steps":16},"light":{"reasoning_effort":"low"},"medium":{}}` | thinking profile table [&lt;case&gt;.profiles.&lt;light\|medium\|heavy&gt;] with model, reasoning_effort, fork_steps; a rule's profile field picks one; an empty field keeps model, rule_reasoning_effort, fork_steps |
| `rule_dirs` | `~/.config/reviewrules/config.toml` | `{}` | more rule folders [&lt;case&gt;.rule_dirs] as name = path; each path holds .reviewrules/config.json, its rules are &lt;name&gt;/&lt;rule id&gt;; a relative path is in the reviewed repo, ~/ is the home folder; a path with no config.json is skipped |
| `parallel` | `~/.config/reviewrules/config.toml` | `8` | forks running at the same time |
| `parent_steps` | `~/.config/reviewrules/config.toml` | `8` | tool-call steps for the parent session |
| `fork_steps` | `~/.config/reviewrules/config.toml` | `8` | tool-call steps for each fork (not used by codex) |
| `plugin_dir` | `~/.config/reviewrules/config.toml` | `~/.config/reviewrules/plugins` | folder of the exec agent plugins, named reviewrules-agent-&lt;agent&gt; |
| `prompts_dir` | `~/.config/reviewrules/config.toml` | `built-in prompts` | folder of prompt files that replace the built-in agent prompts; a file keeps its built-in name; empty uses the built-in prompts |
| `claude_binary` | `~/.config/reviewrules/config.toml` | `"claude"` | claude binary for the claude agent |
| `codex_binary` | `~/.config/reviewrules/config.toml` | `"codex"` | codex binary for the codex agent |
| `prices` | `~/.config/reviewrules/config.toml` | `built in for the default models` | per-model price table [&lt;case&gt;.prices."&lt;model&gt;"] with input, cached, output in USD per 1M tokens; an entry replaces the built-in price |
<!-- config:end -->

## Repo settings

A full `.reviewrules/settings.toml`:

```toml
rule_folders = [".team-rules"]
fail_on = "medium"
fail_on_unanswered = false
correctness = true
max_diff_bytes = 300000

[[rule_sets]]
name = "go"
url = "git@github.com:acme/go-rules.git"
ref = "v1.4.0"
```

- reviewrules searches each name in `rule_folders` in every directory, the same way it searches `.reviewrules`: at the root and in each nested folder. When one directory has more than one rule folder, only one is used: `.reviewrules` first, then the names in the order of the list.
- A repo can have only `.reviewrules/settings.toml` and keep its rules in `.team-rules/config.json`.
- A name in `rule_folders` that is empty, `.`, or that contains `/`, `\`, or `..` is an error: each item MUST be one folder name.
- `correctness = false` turns off the correctness check (the hypothesis generator and its checker forks).

## Rule sets

A rule set is a git repo with `.reviewrules/config.json`, and an optional `.reviewrules/files.json`, at its root. Many repos can use the same rule set. Each `[[rule_sets]]` entry of the repo settings adds one rule set to the review. How to make a rule set repo is in [rule-sets.md](rule-sets.md).

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | unique in the list, `[a-z0-9-]+`; the prefix of each rule id of the set: `go/test-conventions` |
| `url` | yes | any URL that `git fetch` accepts: ssh, https, or `file://` |
| `ref` | yes | a tag, a branch, or a full 40-character commit sha |

- A missing field, an empty field, an unknown field, or a duplicate name is an error. The error names the file, the entry, and the field, and the run stops with exit code 2.
- Before the review starts, reviewrules runs the `git` binary with your own git config and credentials: `git fetch --depth 1 <url> <ref>`. It sets `GIT_TERMINAL_PROMPT=0`, so a fetch that needs a password fails and does not wait for input.
- The cache is `$XDG_CACHE_HOME/reviewrules/rule-sets/<sha256 of url>/`. When `XDG_CACHE_HOME` is not set, the cache is `~/.cache/reviewrules/rule-sets/` on Linux and `~/Library/Caches/reviewrules/rule-sets/` on macOS. Each commit has its own checkout in `commits/<sha>/`.
- A full sha `ref` that the cache has already uses no network. A tag or a branch is fetched on each run.
- When the fetch fails and the cache has a checkout of that `ref`, the review uses it and logs a warning that names the rule set. When the cache has no checkout, the run stops with exit code 2 and an error that names the rule set, the URL, and the git error text.
- Only the root `.reviewrules/` of the rule set is read. Its nested rule folders, its `settings.toml`, and its `ignorePatterns` are not used. Its `instructions` go into the parent prompt after the repo's own instructions, under the header `# Rule set <name> review instructions`.
- The `scope` globs of its rules and docs match paths of the reviewed repo. The doc paths in its `files.json` are read from the rule set checkout, and their text goes into the prompt. The agents can read only the reviewed repo.

## Rule dirs

`rule_dirs` in the user config adds rule folders that are not in git, for example notes that are ignored by git, or rules that apply to every repo you review:

```toml
[local.rule_dirs]
adr = ".notes"
global = "~/.notes"
```

- Each path is a folder that holds `.reviewrules/config.json` and, if needed, `files.json`. It has the layout of a rule set, and its rule ids get the prefix `<name>/`.
- A relative path is in the reviewed repo, and `~/` is the home folder. So one user config line gives every repo its own notes.
- A folder with no `.reviewrules/config.json` is skipped, and the run logs it. A name that a `[[rule_sets]]` entry of the repo settings also has stops the run with exit code 2.
- reviewrules reads the folder as it is at run time, with no fetch and no ref.
- A repo with no `.reviewrules/config.json` of its own still runs when a rule dir or a rule set gives rules. Without both, the run stops with exit code 2.

## User config

A full user config:

```toml
[local]
agent = "codex"
base_url = "http://localhost:4000/v1"
model = "gpt-6-luna"
hypothesis_model = "gpt-6.1-sol"
hypothesis_check_model = ""
reasoning_effort = "medium"
rule_reasoning_effort = "high"
parallel = 4
parent_steps = 8
fork_steps = 8
plugin_dir = "~/.config/reviewrules/plugins"
prompts_dir = ""
claude_binary = "claude"
codex_binary = "codex"

[local.prices."gpt-6-luna"]
input = 0.10
cached = 0.01
output = 0.50

[local.profiles.light]
reasoning_effort = "low"

[local.profiles.heavy]
model = "gpt-6.1-sol"
fork_steps = 16

[ci]
agent = "openrouter"
model = "openai/gpt-6-luna"
hypothesis_model = "openai/gpt-6.1-sol"
parallel = 8
```

Write `plugin_dir` and `prompts_dir` as absolute paths: reviewrules does not expand `~`. A relative path is relative to the current directory of reviewrules.

### Agent prompts

`prompts_dir` is a folder of prompt files. A file whose name matches a built-in prompt replaces that prompt for the run. A missing file keeps the built-in text. An empty `prompts_dir` uses the built-in prompts. Any other file name, an empty file, or a template that does not parse stops the run with exit code 2.

| File | Replaces |
|---|---|
| `review_system.md` | the system prompt of every call |
| `parent_session.md.tmpl` | the parent session |
| `rule_check.md.tmpl` | one rule check |
| `hypothesis_generator.md` | the hypothesis list |
| `hypothesis_check.md.tmpl` | one hypothesis check |
| `json_only_retry.md` | the retry when a reply has no JSON block |
| `last_step_answer.md` | the last tool step of an API backend |

The `.tmpl` files are Go templates with the same fields as the built-in files. A check still answers with the JSON block the built-in prompt names: reviewrules reads the checks, not a free-form verdict.

### Agent detection

With no `agent` key in the used case, reviewrules looks for each backend on the machine:

- `codex` and `claude`: the `codex_binary` and `claude_binary` in `PATH`.
- `fantasy`, `openai`, and `openrouter`: `LITELLM_API_KEY`, `OPENAI_API_KEY`, and `OPENROUTER_API_KEY` set.

One backend found: the run uses it, and `--print-config` shows `# detected: <what it found>`. No backend or several backends found: the run stops with exit code 2, and the error names what it found and the user config file to write `agent` to. An exec plugin is never detected; name it in `agent`.

An unset `model` or `hypothesis_model` takes the default of the agent: `gpt-6-luna` and `gpt-6.1-sol` for the API backends, and the CLI's own default for `claude` and `codex`.

### The `[local]` and `[ci]` cases

The user config has two cases with the same keys. reviewrules uses `[ci]` when the environment variable `CI` is `true`, else `[local]`. A key missing from the used case keeps its default: the cases do not inherit from each other. A key outside the two cases is an error.

### Prices

The cost estimate uses built-in prices for the default models. A `[<case>.prices."<model>"]` table replaces the built-in price of that model, or adds a price for a model with no built-in price. It has three keys, each in USD per 1M tokens:

| Key | Meaning |
|---|---|
| `input` | input tokens that the prompt cache did not serve |
| `cached` | input tokens that the prompt cache served |
| `output` | output tokens, reasoning tokens included |

A model with no price adds nothing to the total, and the summary names it.

## Key errors

reviewrules checks both files before a run. On an error it stops with exit code 2.

- A key that the other file owns gives `config key misplaced: "<key>" in <file> belongs in <owner file>`. For example, `fail_on` in the user config, or `agent` in the repo settings.
- A key that no file owns gives `config key unknown: "<key>" in <file>`. In the user config the key starts with its case, for example `local.agnet`.

## Print the merged config

`reviewrules --print-config` prints every merged value and exits with code 0. It does not run a review. It reads the same files as a run: the repo settings of `--repo`, and the user config of `--config` in the case that `CI` selects. Each line has the key, the value, and the file that gave the value, or `default`:

```text
fail_on                          = "low"                                            # /repo/.reviewrules/settings.toml
correctness                      = true                                             # default
agent                            = "codex"                                          # /home/me/.config/reviewrules/config.toml [local]
prices.gpt-6-luna                = {input = 1.0, cached = 0.5, output = 2.0}        # /home/me/.config/reviewrules/config.toml [local]
rule_sets.go                     = {name = "go", url = "git@github.com:acme/go-rules.git", ref = "v1.4.0", commit = "not fetched"} # /repo/.reviewrules/settings.toml
```

`--print-config` does not fetch the rule sets. Each rule set line shows the commit of the last checkout of its `ref` in the cache, or `not fetched` when the cache has none. For a branch, the remote branch can be at a newer commit.

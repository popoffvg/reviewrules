---
name: run-reviewrules
description: Run reviewrules on a git diff and act on its verdicts — install it, pick a backend, run it on a laptop or in CI, read the summary, the JSON report, and the exit code. Use when asked to "run reviewrules", "review this diff with reviewrules", "check my branch against the .reviewrules rules", or to explain why a reviewrules run failed.
---

# Run reviewrules

reviewrules checks a git diff against the rules in `.reviewrules/` at the repo root, and in the other rule folders that `rule_folders` in `.reviewrules/settings.toml` names. reviewrules has five flags: `--config`, `--repo`, `--base`, `--worktree`, `--json-out`, plus `--print-config`. Every other choice is a key in the repo settings `.reviewrules/settings.toml` or in the user config `~/.config/reviewrules/config.toml`. Every key, its file, its default, and full examples are in `docs/configuration.md`; this skill does not list them.

## Steps

1. **Install.** Run `go install github.com/popoffvg/reviewrules@latest`. It needs Go 1.27 and puts the binary in `$(go env GOPATH)/bin`. Done when `reviewrules --help` prints the usage.
2. **Check the rules.** The repo under review MUST have `.reviewrules/config.json` at its root, or `config.json` in a folder that `rule_folders` in `.reviewrules/settings.toml` names. Done when the file exists. If it does not, stop and tell the user: there is nothing to check.
3. **Pick the backend.** Use the first that works on this machine, and set it as `agent` in the `[local]` case of the user config:
   - `codex` when `codex login status` shows a login;
   - `claude` when the `claude` binary is logged in or `ANTHROPIC_API_KEY` is set (also set `model = "haiku"`, `hypothesis_model = "opus"`, `hypothesis_check_model = "sonnet"`);
   - `openai` with `OPENAI_API_KEY`, or `openrouter` with `OPENROUTER_API_KEY`;
   - `fantasy` only when a LiteLLM proxy runs at `base_url`.

   Do not edit the operator's own `~/.config/reviewrules/config.toml` without asking: write a new file and pass it with `--config`. Done when `reviewrules --print-config` shows the `agent` you picked and exits with code 0.
4. **Run it.** From the repo root, in the background (a run takes minutes):
   - before a push: `reviewrules --base origin/main --worktree`;
   - the commits only, as CI sees them: drop `--worktree`.

   A run checks every rule in scope. Done when the process exits.
5. **Read the result.** Read the exit code first, then the summary.

## Exit codes

| Code | Meaning | What to do |
|---|---|---|
| `0` | the review passed | report the pass and the cost line |
| `1` | the review failed | read `Review failed because:` and fix each reason |
| `2` | the tool could not run | read the log on stderr: a bad flag, a missing API key, a bad or misplaced config key (`config key unknown:`, `config key misplaced:`), or a diff larger than `max_diff_bytes` |

## Summary lines

- `PASS`, `FAIL`, or `WARN` per rule check, with the severity and the rule id. Under a `FAIL`, each finding is `file:line — message`. `WARN` means the model gave no answer; it fails the run only with `fail_on_unanswered = true` in the repo settings.
- `CONFIRMED`, `REFUTED`, or `UNSURE` per correctness hypothesis. A `CONFIRMED` line shows the input that breaks the code, the evidence, and the fix. A `CONFIRMED` hypothesis fails the run.
- `usage:` for the total, then one `usage:` line per part (`parent`, `rules`, `correctness`) and model with its cost, then `cost estimate: total`. A model with no price asks for a `prices."<model>"` table in the user config.

A `FAIL` below `fail_on` (default `medium`) does not fail the run. Report it, but do not block on it.

## CI

With `CI=true` the tool reads the `[ci]` case of the user config (the job writes it, or passes `--config` with a committed file), writes the JSON report to stdout, and writes the summary to stderr. Use `--json-out <file>` to keep stdout free. The CI job MUST check out the full history (`fetch-depth: 0`) and pin the install to a commit SHA or a tag: `go install github.com/popoffvg/reviewrules@<sha or tag>`. The full workflow is in `docs/ci.md`.

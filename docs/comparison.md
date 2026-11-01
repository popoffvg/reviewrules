# reviewrules and a plain Codex review

reviewrules runs a fixed plan: a parent session reads the diff, each scoped rule gets its own fork, correctness checks run in separate forks, and the tool applies your severity policy. A plain Codex session can review the same diff and rules in one conversation.

| | Plain Codex review | reviewrules |
|---|---|---|
| Review task | One session gets the diff and all rules. | A parent reads the diff; each scoped rule gets a fork. |
| Rule coverage | You ask the model to cover every rule. | Each rule has a structured verdict; a missing answer is visible. |
| Correctness | The same session looks for bugs. | A generator proposes hypotheses; separate forks check them. |
| Model choice | One model for the session. | Choose models for rules, generation, and checking separately. |
| Output | Whatever format the prompt asks for. | Rule verdicts, evidence, correctness results, JSON, usage, exit codes. |
| CI policy | Your integration interprets the answer. | `fail_on` and `fail_on_unanswered` decide pass or fail. |

Use plain Codex for a quick second opinion. Use reviewrules when the same repository policy should run on every branch and CI needs a repeatable result.

## Controlled example in this repository

The fixture lives under [`docs/comparison/testdata/`](comparison/testdata/): a small Go diff and six scoped rules with deliberate defects. Reference tests in the same folder define what “found” means; they are not part of the reviewed tree when you reproduce the run.

On **2026-11-01**, with that fixture and high reasoning effort:

| Setup | Seeded defects | Extra defect (verified by test) | Wall time | Total input + output tokens |
|---|---:|---:|---:|---:|
| Plain Codex (`gpt-6.1-sol`, one `codex exec`) | 6 of 6 | 1 | 47 s | 115,514 |
| reviewrules via LiteLLM (`gpt-6-luna` rules/checks, `gpt-6.1-sol` generator) | 6 of 6 | 0 | 44 s | 76,425 |
| reviewrules via Codex CLI (Sol for every task) | 6 of 6 | 0 | 92 s | 995,659 |

Plain Codex exited 0 because the session finished; reviewrules exited 1 because the configured policy failed on the violations it reported. Token counts are one run each, not a cost quote. Numbers are in [`measurement.json`](comparison/results/measurement.json).

The API backend and the Codex CLI backend load different prompts and tools, so token totals are not directly comparable except as rough order-of-magnitude. Forks reuse parent history for caching, but each fork still reads that history.

## Reproduce

Follow [`docs/comparison/README.md`](comparison/README.md). You need reviewrules, the fixture files from this repo, and either a logged-in Codex CLI or a local LiteLLM proxy as in [`litellm.md`](litellm.md).

## Limits

One synthetic diff and one run per setup. Results do not predict quality, latency, or cost on your repositories. The baseline prompt asked for per-rule evidence; a shorter “review this diff” prompt may behave differently.

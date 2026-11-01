# reviewrules and a plain Codex review

reviewrules adds a repeatable review plan around an LLM: one check per scoped rule, separate correctness hypotheses and checkers, and a report with a configured failure policy. A plain Codex session can review the same diff and rules in one conversation.

| | Plain Codex review | reviewrules |
|---|---|---|
| Review task | One session receives the diff and all the rules. | A parent reads the diff; each scoped rule gets a fork. |
| Rule coverage | Ask the reviewer to address every rule. | Each scoped rule has a structured verdict; a missing answer is visible. |
| Correctness | The same session looks for bugs. | A generator proposes hypotheses; separate forks check them. |
| Model choice | One model for the review session. | Choose the rule, generator, and checker models separately. |
| Output | The prompt chooses the response format. | Rule verdicts, evidence, correctness results, JSON, usage, and exit codes. |
| CI policy | Your integration interprets the answer. | Severity thresholds and unanswered-check policy determine pass or fail. |

Use a plain Codex review for an ad hoc second opinion. Use reviewrules when you want the same repository policy checked on every branch and a result that CI can act on.

## Measured example: Luna rules, Sol hypotheses

On one small synthetic change, **both approaches found all six seeded defects**. The reviewrules run through LiteLLM used about **34% fewer total tokens**. Plain Codex also found one extra defect that reviewrules missed.

| Measurement | Plain Codex | reviewrules through LiteLLM |
|---|---|---|
| Rule and checker model | `gpt-6.1-sol` | `gpt-6-luna` |
| Hypothesis generator | Same Codex session | `gpt-6.1-sol` |
| Seeded defects recovered | 6 of 6 | 6 of 6 |
| Clean rule checks correctly passed | 2 of 2 | 2 of 2 |
| Additional defects verified by a new test | 1 | 0 |
| Wall time | 47.4 s | 43.5 s |
| Uncached input tokens | 23,645 | 49,842 |
| Cached input tokens | 89,984 | 20,992 |
| Output tokens, including reasoning | 1,885 | 5,591 |
| **Total input and output tokens** | **115,514** | **76,425** |
| Process exit code | 0: session completed | 1: review policy failed |

Fewer total tokens does not imply fewer uncached tokens: the LiteLLM run used about twice as much uncached input. Most of its work used Luna. Its Sol generator used 3,370 uncached input tokens and 567 output tokens. Dollar spend was not measured.

Plain Codex returned JSON because the prompt requested it. Its successful process exit did not mean the code passed review. reviewrules emitted its standard report and failed the configured policy because it found violations.

## Setup and reference findings

The runs took place on **November 1, 2026**. Both used the same six repository rules and 2,191-byte diff, adding 94 lines to a one-line Go file. Reference checks were written before either review. The reviewers could read the fixture repository but could not see the reference tests.

The plain baseline was Codex CLI **0.166.0**, using one `codex exec` session with **`gpt-6.1-sol` at high effort**. It was explicitly prompted to check every rule and find other correctness defects.

reviewrules used the **LiteLLM API backend**. Its parent, six rule checks, and four hypothesis checkers used **`gpt-6-luna`**. Its hypothesis generator used **`gpt-6.1-sol`**. All used high effort, with `parallel = 4`.

| Reference defect | Demonstrating input | Plain Codex | reviewrules |
|---|---|---|---|
| Per-unit rounding | Unit price 0.335 × quantity 3 gives 102 cents instead of 101. | Found | Rule FAIL |
| Lost caller context | A canceled caller context is replaced by a background context. | Found | Rule FAIL; H4 confirmed |
| Tenant cache collision | Store for tenant A, then load the same SKU for tenant B. | Found | Rule FAIL; H3 confirmed |
| Reordered price ties | Equal-price offers arrive as Z, A and are returned as A, Z. | Found | Rule FAIL |
| Missing final page | Two offers, offset 1, size 1 returns no items. | Found | H1 confirmed |
| Integer discount division | 1,000 cents with a 10% discount returns 0 instead of 900. | Found | H2 confirmed |

Both reviewers passed the two clean checks: input-slice ownership and lookup-error wrapping. All six defect tests failed before correction. Those tests and the two clean checks passed after six fixes.

Plain Codex also reported a pagination overflow when a valid nonnegative page size plus the offset exceeds the largest `int`. A new test reproduced the panic. Capping the size before addition made the test pass. This additional finding is separate from the six seeded defects.

The twelve reviewrules tasks were one parent, six rule forks, one generator, and four hypothesis checkers. Two confirmed hypotheses duplicated issues already found by rule forks. The tenant-cache rule produced three line-level findings for one defect. The table counts unique defects.

## Why the backend changes input volume

The API backend sends a compact reviewer prompt and three repository-reading tools. The local Codex runs also loaded CLI instructions, tool definitions, and installed skills. The logs show both the plain Codex session and the reviewrules parent reading the local codebase-memory skill. A fork inherits the parent history, then adds its own instructions and tool results.

An additional run kept **Sol for every reviewrules task through the Codex CLI**:

| Measurement | Plain Codex, Sol | reviewrules + Codex CLI, Sol |
|---|---|---|
| Seeded defects recovered | 6 of 6 | 6 of 6 |
| Wall time | 47.4 s | 92.1 s |
| CLI invocations | 1 | 12 |
| Model requests, including tool continuations | 4 | 26 |
| Uncached input tokens | 23,645 | 363,827 |
| Cached input tokens | 89,984 | 625,152 |
| Output tokens, including reasoning | 1,885 | 6,680 |
| **Total input and output tokens** | **115,514** | **995,659** |

The per-request logs record about 25–31K input tokens per plain Codex request and about 40K for most requests in the Codex rule and hypothesis forks. The API rule forks reported about 3.5K input tokens each. The CLI tasks made repeated requests with more context. A fork shares history; it still consumes input when the model reads that history.

## Reproduce and inspect

- [Inputs and reproduction steps](comparison/README.md), including the baseline prompt, rule config, source fixture, and reference tests.
- [Recorded measurements](comparison/results/measurement.json) and [exact baseline input](comparison/results/baseline-input.md).
- [Plain Codex answer](comparison/results/plain-codex.json) and [LiteLLM reviewrules report](comparison/results/reviewrules-litellm.json).
- The additional Codex-backend run: [findings extract](comparison/results/reviewrules-codex-findings.json) and [per-request usage records](comparison/results/per-request-audit.json).
- Reference test output: [defective version](comparison/results/verification-broken.jsonl) and [corrected version](comparison/results/verification-fixed.jsonl).
- Additional overflow check: [panic reproduced](comparison/results/additional-broken.jsonl) and [correction verified](comparison/results/additional-fixed.jsonl).

Codex usage is summed from per-request session records. API usage is summed from the provider-reported request usage. Cached input is shown separately from uncached input. Output includes reasoning tokens. The API report contains a configured-price estimate; it is not a measured bill.

## Limits

This was one run per setup on one small synthetic change. It does not establish typical quality, latency, or cost across real projects. Plain Codex ran first, followed by reviewrules with Sol through Codex, then reviewrules with Luna/Sol through LiteLLM. Cache state and local configuration were not reset between runs.

The main comparison changes the backend and model mix together. It measures two practical setups, so it cannot attribute the result solely to rule forks or model choice. The API and CLI backends expose different tools and instructions. The baseline prompt requested per-rule evidence, which a casual “review this” request might omit.

On this input, Luna/Sol through LiteLLM recovered the same six seeded defects with fewer total tokens. Plain Codex found one more distinct defect. Choose reviewrules for explicit rule coverage, separate checks, model choice per task, and a reusable CI policy.

# Read a review result

The terminal summary lists each rule check with its severity and rule ID:

| Status | Meaning |
|---|---|
| `PASS` | The check found no violation. |
| `FAIL` | The check reports findings, each with `file:line` and a message. |
| `WARN` | The model gave no usable answer. The JSON rule verdict is `ERROR`. |

Correctness hypotheses get `CONFIRMED`, `REFUTED`, or `UNSURE`. A confirmed hypothesis includes the input that breaks the code, evidence lines, and a proposed fix. Treat findings as review evidence to inspect.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | The review passed its configured policy. |
| `1` | A rule failed at or above `fail_on`, a correctness hypothesis was confirmed, or `fail_on_unanswered` made an unanswered check fail. |
| `2` | The tool could not run, for example because of invalid config, a missing backend, or an oversized diff. |

`fail_on` defaults to `medium`. An unanswered rule or hypothesis generator error is a warning by default. With `fail_on_unanswered = true`, unanswered rules at or above `fail_on` and generator errors fail the review. The final `Review failed because:` section names the reasons.

## Usage and cost

The summary prints total token usage, then usage and estimated cost per part (`parent`, `rules`, `correctness`) and model. It ends with the total cost estimate. A model with no configured price contributes nothing to that estimate and is named as unpriced.

The estimate is not a bill. Subscription runs consume plan allowances. The Claude estimate excludes cache writes. Configure per-model prices in the [user config](configuration.md#prices).

## JSON report

```bash
reviewrules --base origin/main --json-out report.json
reviewrules --base origin/main --json-out -
```

With `CI=true` and no `--json-out`, the report goes to stdout. Whenever JSON takes stdout, the human summary goes to stderr.

The correctness usage is available as `correctness.generator_usage`, `correctness.check_usage`, and `correctness.usage`. `cost_estimate.per_part_usd.correctness` holds its cost. `cost_estimate.parts` has one row per part and model. `total_usage` and `cost_estimate.total_usd` cover the whole run.

## Correctness check

The correctness check runs by default. One generator fork writes up to 10 yes/no questions about bugs the diff could cause. Each question gets a separate checker fork, which reads the parent session rather than the generator's history.

This adds up to 11 forks. Choose the generator and checker models separately with `hypothesis_model` and `hypothesis_check_model`. To turn correctness off, set `correctness = false` in `.reviewrules/settings.toml`. See [how a run works](how-a-run-works.md) for the full sequence.

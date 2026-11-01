# Comparison inputs

These files reproduce the small example in [the comparison](../comparison.md). `testdata/offers.go` intentionally contains defects. It is an evaluation input, not application code.

## Prepare the reviewed repository

In a temporary directory, create a Git repository with a `main` branch. Its initial commit contains:

- `go.mod`: module `comparison.example/offers`, Go `1.27`;
- `offers.go`: only `package offers`;
- `.reviewrules/config.json`: a copy of [rules.json](testdata/rules.json).

Replace `offers.go` with [the fixture](testdata/offers.go). Leave it uncommitted. Create `.reviewrules/settings.toml` with `fail_on_unanswered = true`.

Keep reference tests outside the reviewed repository. Both reviewers can inspect every file in that repository, so placing answers there would contaminate the experiment.

## Plain Codex input

Use [the baseline prompt](baseline-prompt.md), with the rule config and diff appended. The [exact recorded input](results/baseline-input.md) is available. Run one `codex exec` session with:

```text
--json --skip-git-repo-check -s read-only
-c project_doc_max_bytes=0
-m gpt-6.1-sol
-c model_reasoning_effort=high
```

Set `developer_instructions` to the contents of [`internal/prompts/review_system.md`](../../internal/prompts/review_system.md). The recorded run used `background/launch.Run` for process logging.

## reviewrules through LiteLLM

Start the proxy using [the LiteLLM guide](../litellm.md). Use [reviewrules.toml](testdata/reviewrules.toml), which selects Luna for the parent, rules, and hypothesis checks, and Sol for hypothesis generation.

From the temporary repository, with `LITELLM_API_KEY` set to the proxy key:

```bash
reviewrules --repo /path/to/temporary/repo --base main --worktree \
  --config /path/to/reviewrules.toml --json-out /path/outside/repo/report.json
```

For the additional all-Sol Codex-backend run, use [reviewrules-codex-sol.toml](testdata/reviewrules-codex-sol.toml) instead. Run setups sequentially. Capture wall time, final answers, the JSON report, and per-request Codex session usage. The model-request count differs from the CLI-invocation count because tools require continuations.

## Reference checks

Copy `go.mod` and the fixture into another temporary directory. Copy [verification_test.go.txt](testdata/verification_test.go.txt) there as `offers_test.go`. Run `go test -json -count=1 ./...` in the background and save its output outside the reviewed repository.

Expected failures are `TestMoneyRounding`, `TestCallerContext`, `TestTenantCache`, `TestOfferTies`, `TestPageBoundary`, and `TestPercentageDiscount`. `TestInputOwnership` and `TestLookupErrors` must pass.

For the positive control, make these six corrections in that separate copy:

| Defect | Correction |
|---|---|
| Per-unit rounding | Multiply price by quantity before rounding to cents. |
| Lost context | Pass `ctx` to `LookupQuote`. |
| Tenant cache collision | Use both `TenantID` and `SKU` in the map key. |
| Reordered ties | Use a stable price-only sort. |
| Lost final page | Clamp an end beyond the length; allow an end equal to the length. |
| Integer discount division | Multiply total by the remaining percentage before dividing by 100. |

All eight checks must then pass. Match findings to defect behavior, not line proximity. Count duplicates once. Inspect additional findings separately; the six seeded defects are not an exhaustive catalogue of possible input extremes.

The extra overflow finding was verified after the reviews using [additional_checks_test.go.txt](testdata/additional_checks_test.go.txt). Cap `size` to `len(offers)-int(offset)` before calculating `offset + size` to fix it. This check was not part of the original six-defect reference set.

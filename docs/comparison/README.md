# Reproduce the comparison example

This directory holds the [controlled example](../comparison.md): rules, fixture source, configs, and reference tests. Nothing here is production code for reviewrules itself.

## 1. Build a temporary repository

In an empty directory:

1. `git init` and commit `main` with `go.mod` (`module comparison.example/offers`, Go 1.27), `offers.go` containing only `package offers`, and `.reviewrules/config.json` copied from [`testdata/rules.json`](testdata/rules.json).
2. Replace `offers.go` with [`testdata/offers.go`](testdata/offers.go) and leave it uncommitted.
3. Add `.reviewrules/settings.toml` with `fail_on_unanswered = true`.

Keep [`testdata/verification_test.go.txt`](testdata/verification_test.go.txt) **outside** that repository when you run the reviews.

## 2. Plain Codex baseline

Use [`baseline-prompt.md`](baseline-prompt.md) plus the rule JSON and diff. Run one read-only `codex exec` with `gpt-6.1-sol` at high effort. Set `developer_instructions` from [`internal/prompts/review_system.md`](../../internal/prompts/review_system.md) and `-c project_doc_max_bytes=0`.

## 3. reviewrules

**LiteLLM:** start the proxy per [`litellm.md`](../litellm.md), set `LITELLM_API_KEY`, and run with [`testdata/reviewrules.toml`](testdata/reviewrules.toml).

**Codex CLI:** use [`testdata/reviewrules-codex-sol.toml`](testdata/reviewrules-codex-sol.toml) and `agent = "codex"` in your user config.

Example:

```bash
reviewrules --repo /path/to/temp/repo --base main --worktree \
  --config /path/to/reviewrules.toml --json-out /tmp/report.json
```

## 4. Check findings (optional)

Copy the fixture and `go.mod` to another directory, add the verification test file as `offers_test.go`, and run `go test ./...`. Six tests should fail on the buggy fixture and pass after the fixes described in the test file. The overflow check in [`testdata/additional_checks_test.go.txt`](testdata/additional_checks_test.go.txt) is separate from the six seeded defects.

Recorded numbers for the run that produced this doc set are in [`results/measurement.json`](results/measurement.json).

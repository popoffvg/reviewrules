# Run in CI

With `CI=true` the tool reads the `[ci]` case of the user config and writes the JSON report to stdout; the summary goes to stderr. The diff needs the base branch history, so fetch it fully.

```yaml
name: Review
on: pull_request

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

Replace `<sha or tag>` with a commit SHA or a tag of reviewrules, so that each run uses the same version. `actions/setup-go` puts `$(go env GOPATH)/bin` on `PATH`. Use commit-pinned actions, as in this repo's `.github/workflows/ci.yaml`. To run on a ChatGPT subscription instead, host the LiteLLM proxy near the runners ([shared proxy setup](../deploy/litellm/README.md)) and set `agent = "fantasy"` and `base_url = "<proxy>/v1"` in the `[ci]` case, with `LITELLM_API_KEY`. The job can also pass `--config` with a user config that the repo commits.

## Decide what fails the job

Commit the policy in `.reviewrules/settings.toml`:

```toml
fail_on = "medium"
fail_on_unanswered = true
```

`fail_on_unanswered` makes missing rule answers at or above `fail_on`, and hypothesis generator errors, fail the run. Without it, those errors are warnings. A confirmed correctness hypothesis always fails the review. See [review results](review-results.md) for all exit codes.

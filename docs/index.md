# Getting started

reviewrules checks a Git diff against rules you write in plain language. Each rule in scope gets its own verdict. Use the same rules locally and in CI.

## Install

[Install reviewrules](installation.md) and the Codex CLI. Run `codex login`.

## Configure

Add these settings to `~/.config/reviewrules/config.toml`. If the file already has a `[local]` section, add `agent` to that section. For Claude Code or a direct API, see [backend setup](backends.md).

```toml
[local]
agent = "codex"
```

## Add a rule

Create `.reviewrules/config.json` at the root of your repository. This rule checks that new error messages in Go files name the operation that failed.

```json
{
  "rules": [
    {
      "id": "error-context",
      "rule": "Every new error must name the operation that failed.",
      "scope": ["**/*.go"],
      "severity": "medium"
    }
  ]
}
```

## Review a diff

Run this command from the repository root.

```sh
reviewrules --base origin/main --worktree
```

Replace `origin/main` with your base branch. Stage new files with `git add` to include them in the review.

Find `error-context` in the summary. A `FAIL` lists the findings and any file locations the reviewer provides. The rule runs when the diff changes a Go file. See [review results](review-results.md).

## Next steps

- [Run from a coding agent](coding-agents.md) to start a review in Claude Code or Codex.
- [Run in CI](ci.md) to check pull requests.
- [Write rules](writing-rules.md) for your team’s conventions.
- [Share rule sets](rule-sets.md) across repositories.
- [Reviewrules website](https://popoffvg.github.io/reviewrules/).

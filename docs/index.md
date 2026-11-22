# Getting started

reviewrules checks a Git diff against rules you write in plain language. Each rule in scope gets its own verdict. You can run the same rules locally and in CI.

## Install

[Install reviewrules](installation.md) and the Codex CLI. Run `codex login`.

## Configure

Add the following to `~/.config/reviewrules/config.toml`. Use the existing `[local]` section if the file already has one. To use Claude Code or an API, see [backend setup](backends.md).

```toml
[local]
agent = "codex"
```

## Add a rule

In the repository you want to review, create `.reviewrules/config.json`. This example checks Go files for error messages that omit the failed operation.

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

Replace `origin/main` with your base branch. `--worktree` includes staged and unstaged edits to tracked files. Stage any new files you want to review.

Read the verdict for `error-context` in the terminal. A violation includes a message and a file location when one is available. If the diff contains no Go files, the rule does not run. See [review results](review-results.md) for verdicts and exit codes.

## Where to go next

- [Write rules](writing-rules.md) for conventions that need code context.
- [Share a rule set](rule-sets.md) across team repositories.
- [Run from a coding agent](coding-agents.md) in Claude Code or Codex.
- [Run in CI](ci.md) on each pull request.

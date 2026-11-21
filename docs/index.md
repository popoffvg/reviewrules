# Quick start

reviewrules checks Git diffs against rules you write in plain language. This example uses Go 1.27 or later, Git, and the Codex CLI.

## Install

```sh
go install github.com/popoffvg/reviewrules@latest
export PATH="$(go env GOPATH)/bin:$PATH"
codex login
```

## Configure

Add this to `~/.config/reviewrules/config.toml`.

```toml
[local]
agent = "codex"
```

Create `.reviewrules/config.json` in your repository.

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

```sh
reviewrules --base origin/main --worktree
```

Replace `origin/main` with your base branch. See [how to read the result](review-results.md).

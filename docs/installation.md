# Install reviewrules

Install Git and Go 1.27.3 or later. For local reviews through a subscription, install either the Codex CLI or Claude Code.

```sh
go install github.com/popoffvg/reviewrules@latest
```

If your shell cannot find `reviewrules`, add the Go binary directory to `PATH`. Go uses `GOBIN` when you set it. Otherwise, it uses `$(go env GOPATH)/bin`.

```sh
go_bin=$(go env GOBIN)
export PATH="${go_bin:-$(go env GOPATH)/bin}:$PATH"
reviewrules --help
```

Continue with [your first review](index.md). In CI, [pin the installation](ci.md) to a tag or commit.

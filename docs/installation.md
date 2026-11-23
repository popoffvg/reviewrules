# Install reviewrules

Install Git and Go 1.27.3 or later. For local reviews through a subscription, install the Codex CLI or Claude Code.

```sh
go install github.com/popoffvg/reviewrules@latest
```

Go installs `reviewrules` in `GOBIN` if you set it, or in `$(go env GOPATH)/bin` otherwise. If your shell cannot find `reviewrules`, add that directory to `PATH`.

```sh
go_bin=$(go env GOBIN)
export PATH="${go_bin:-$(go env GOPATH)/bin}:$PATH"
reviewrules --help
```

Continue with [your first review](index.md). In CI, [pin the installation](ci.md) to a tag or commit.

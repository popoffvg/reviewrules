# CLI commands

Run `reviewrules` from the repository root. The diff starts at the merge base of the base ref and `HEAD`.

| Task | Command |
|---|---|
| Review committed changes | `reviewrules --base origin/main` |
| Include staged and unstaged edits | `reviewrules --base origin/main --worktree` |
| Review another repository | `reviewrules --repo /path/to/repo` |
| Save a JSON report | `reviewrules --json-out report.json` |
| Inspect effective configuration | `reviewrules --print-config` |
| List all flags | `reviewrules --help` |

Stage new files before a `--worktree` review. Replace `origin/main` with the ref you want to compare against.

Use [configuration](configuration.md) to change the backend or review policy. See [review results](review-results.md#exit-codes) for exit codes.

# CLI commands

Run `reviewrules` from the repository root. The review compares the merge base of the base ref and `HEAD` with `HEAD` by default.

| Task | Command |
|---|---|
| Review committed changes | `reviewrules --base origin/main` |
| Include staged and unstaged edits | `reviewrules --base origin/main --worktree` |
| Review another repository | `reviewrules --repo /path/to/repo` |
| Use a specific user config file | `reviewrules --config /path/to/config.toml` |
| Save a JSON report | `reviewrules --json-out report.json` |
| Save API backend sessions | `reviewrules --session-dir sessions` |
| Inspect effective configuration | `reviewrules --print-config` |
| List all flags | `reviewrules --help` |

Stage new files before a `--worktree` review. Set `--base` to the ref your branch targets.

See [configuration](configuration.md) for backend and policy settings. See [exit codes](review-results.md#exit-codes) for the result of a run.

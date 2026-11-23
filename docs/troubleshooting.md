# Troubleshooting

## The shell cannot find `reviewrules`

Check where Go installed the binary. Add that directory to `PATH`. [Install reviewrules](installation.md)

## Git cannot find the base ref

Check that `--base` names a ref that exists in the local clone. Fetch the target branch if its remote-tracking ref is missing. In CI, check out the full history so Git can find the merge base. [Run in CI](ci.md)

## The diff is empty or omits local edits

Pass `--worktree` to include staged and unstaged edits. Stage new files to include them. Check `ignorePatterns` if a changed path is missing. [Write rules](writing-rules.md)

## A rule does not run

Check that the rule’s `scope` matches a changed path. Check that reviewrules loads its rule folder. The JSON report lists `rule_dirs` and `skipped_rule_ids`. [Write rules](writing-rules.md)

## The backend fails to start

Run `reviewrules --print-config` to check the selected backend and the source of each setting. Check the CLI login or API key for that backend. [Choose a backend](backends.md)

# Write the rules

Put a `config.json` in `.reviewrules/` at the repo root, or in a folder that `rule_folders` names (see [Rule folders](#rule-folders)).

```json
{
  "instructions": "Go backend. Errors use the mierr package.",
  "ignorePatterns": "**/*.pb.go\n**/testdata/**",
  "rules": [
    {
      "id": "test-conventions",
      "rule": "Tests: use testing.TB; use t.Cleanup for cleanup; use t.Context(); use t.TempDir(); use '-' instead of spaces in t.Run titles",
      "scope": ["**/*_test.go"],
      "severity": "medium"
    }
  ]
}
```

- `scope` is a list of globs. A rule whose scope matches no changed file is skipped.
- `severity` is `low`, `medium`, or `high`. `fail_on` in the repo settings sets the lowest severity that fails the run.
- A rule written as `lead: a; b; c` with 3 or more short items is split into one check per item (`test-conventions#1`, `#2`, …). Each item gets its own fork.
- `files.json` lists the docs the rules refer to (`path`, `description`, `scope`). A doc whose scope matches a changed file goes into the parent session.
- A nested folder can have its own rule folder with more rules. Only the root's `instructions` and `ignorePatterns` are used.
- An untracked root rule folder also counts, so you can try rules before you commit them. A nested rule folder counts only when git tracks it.

## Rule folders

`rule_folders` in `.reviewrules/settings.toml` at the repo root adds more rule folder names:

```toml
rule_folders = [".team-rules"]
```

- reviewrules searches each name in `rule_folders` in every directory, the same way it searches `.reviewrules`: at the root and in each nested folder.
- When one directory has more than one rule folder, only one is used: `.reviewrules` first, then the names in the order of the list.
- The file is optional. A repo can have only `.reviewrules/settings.toml` and keep its rules in `.team-rules/config.json`.
- A name that is empty, `.`, or that contains `/`, `\`, or `..` is an error: each item MUST be one folder name. On an error the run stops with exit code 2.

## Share rules across repositories

Use [`[[rule_sets]]`](rule-sets.md) in `.reviewrules/settings.toml` to load a shared rule repository. Each set prefixes its rule IDs with its name, so findings keep their source.

Use a linter for checks it can express reliably. Save LLM rules for decisions that need code context. The [find-lintable-rules skill](../coding-agent-plugin/skills/find-lintable-rules/SKILL.md) helps sort an existing rule set.

---
name: find-lintable-rules
description: Read every reviewrules rule and say, per rule, whether a linter can check it instead of an LLM fork — move it to a linter, split it, or keep it. Use when asked "which rules can be linters", "move rules to the linter", "make the review cheaper", "is this rule lintable", or when a rule fails often on the same syntax.
---

# Find lintable rules

Each rule check costs one LLM fork in each run, and its verdict can change from run to run. A linter check costs nothing and gives the same answer each time. Move a rule to a linter when a linter can check it with no judgment. Keep the rest in reviewrules.

This skill gives a verdict and a draft. It does not edit the rules or the linter config until the user says so.

## Steps

1. **Collect the rules.** Read `.reviewrules/config.json`, the `config.json` in each folder that `rule_folders` in `.reviewrules/settings.toml` names, and the root `.reviewrules/config.json` of each `[[rule_sets]]` checkout. Split a checklist rule into its items (`id#1`, `id#2`), as reviewrules does. Done when you have a list of rule ids with the rule text and the `scope` of each.
2. **Find the linters.** List the linters the repo runs and their config: for example `.golangci.yaml` and `.ruleguard/`, `eslint.config.*`, `ruff.toml` or `pyproject.toml`, `.semgrep.yml`, `sgconfig.yml` (ast-grep), `.markdownlint*`. Also read the CI workflow and the task file (`Taskfile.yml`, `Makefile`, `package.json`) to see which of them run. Done when you know each linter that runs, and how a new check gets into it.
3. **Give each rule a verdict.** Use the table in [Verdicts](#verdicts). Done when each rule id has one verdict and one reason.
4. **Write the draft for each `linter` and `split` verdict.** Name an existing linter rule when one exists (for example a golangci-lint linter or an ESLint rule) and the config line that turns it on. When none exists, write the new check: a ruleguard matcher, a semgrep or ast-grep pattern, or a custom rule. For a `split`, write the new rule text that stays in reviewrules. Done when each draft has the tool, the config or the pattern, and the message.
5. **Test each draft against a bad example.** Write a short bad example from the rule text, or find a real one with `git log -S`. Run the linter on it. The check MUST report it. Then run the linter on the whole repo and count the new reports. Done when each draft reports the bad example. A draft that reports nothing proves nothing. A draft with many false reports goes back to `keep`.
6. **Report.** Give the table in [Report](#report). Then ask the user which drafts to apply. When the user says yes, add the check to the linter, and remove the rule from `config.json` or write its new text. Done when the linter passes on the repo and `jq . config.json` exits with code 0 for each changed rule file.

## Verdicts

| Verdict | When | Examples |
|---|---|---|
| `linter` | The rule names a syntax shape that you can see in one file: a call, an import, a type, a literal, a name pattern, a file path. A report is wrong only in rare cases. | "use `t.Context()` in tests, not `context.Background()`"; "no `fmt.Println` outside `cmd/`"; "a `t.Run` title has no spaces" |
| `split` | One part of the rule is a syntax shape, and the other part needs judgment. | "wrap a returned error with `%w` and add the operation name": `%w` is a linter check, a useful operation name is not |
| `keep` | The rule needs intent, meaning, or knowledge of more than one file: a good name, the right layer, a comment that matches the code, a missing test for a new branch, a design choice. Also keep a rule when the only possible check gives many false reports. | "a comment says why, not what"; "a new public function has a test"; "a handler does not call the database directly" |

Signs that a rule is `keep`: its text has "good", "clear", "when needed", "if it makes sense", or "prefer". Signs that a rule is `linter`: its text has "never", "always", or "MUST NOT" next to a code token in backticks.

## Report

One row per rule id, ordered `linter`, `split`, `keep`:

| Rule id | Verdict | Reason | Tool and draft | Bad example reported | Repo reports | Rule text that stays |
|---|---|---|---|---|---|---|

- **Tool and draft**: the existing linter rule and its config line, or the path of the new pattern file.
- **Bad example reported**: yes or no, from step 5.
- **Repo reports**: the number of reports on the current repo. More than zero means the repo breaks the rule today; list the files.
- **Rule text that stays**: the new text for a `split`; empty for `linter`; the same text for `keep`.

End with the number of rule checks that the drafts remove from each run.

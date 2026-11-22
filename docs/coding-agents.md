# Run from a coding agent

Install the reviewrules plugin to start a review from a Claude Code or Codex session. Set up the [review backend](backends.md) before your first run.

## Claude Code

```sh
claude plugin marketplace add popoffvg/reviewrules
claude plugin install reviewrules@reviewrules
```

## Codex

```sh
codex plugin marketplace add popoffvg/reviewrules
codex plugin add reviewrules@reviewrules
```

To install from a local clone, replace `popoffvg/reviewrules` with the path to the clone. Open a session in the repository you want to review and ask the agent to review your branch with reviewrules.

## Included skills

- [Run a review and read the result](https://github.com/popoffvg/reviewrules/blob/main/coding-agent-plugin/skills/run-reviewrules/SKILL.md).
- [Create an exec agent plugin](https://github.com/popoffvg/reviewrules/blob/main/coding-agent-plugin/skills/create-agent-plugin/SKILL.md).
- [Find rules a linter can check](https://github.com/popoffvg/reviewrules/blob/main/coding-agent-plugin/skills/find-lintable-rules/SKILL.md).

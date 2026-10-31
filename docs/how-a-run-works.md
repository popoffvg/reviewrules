# How a run works

A run of reviewrules does these steps in this order:

1. Load the rules and the docs from the rule folders. Drop each rule whose `scope` matches no changed file.
2. Diff `merge-base(--base, HEAD)` against `HEAD`, or against the working tree with `--worktree`. Paths in `ignorePatterns` are left out.
3. Run the **parent session** once. Its first message holds the instructions, the scoped docs, and the diff. The parent maps the change and gives no verdict.
4. Run every rule check as a **fork** of the parent session. A fork is a new call that starts with the parent's full history, so the provider serves that history from its prompt cache.
5. Run one fork that writes at most 10 yes/no correctness hypotheses. Then answer each hypothesis in its own fork of the parent, not of the generator, so a checker does not inherit the generator's suspicion.
6. Print the summary, write the JSON report when asked, and exit.

Every agent can only read. The API backends get `read_file`, `grep` (git grep), and `list_dir`, all confined to the repo; `claude` gets `Read`, `Grep`, `Glob`; `codex` runs with `-s read-only`.

The backend that runs the parent session and the forks is one of the agents in [agent-plugins.md](agent-plugins.md).

# How a run works

A run of reviewrules does these steps in this order:

1. Load the rules and the docs from the rule folders. Drop each rule whose `scope` matches no changed file.
2. Diff `merge-base(--base, HEAD)` against `HEAD`, or against the working tree with `--worktree`. Paths in `ignorePatterns` are left out.
3. Run the **parent session** once. Its first message holds the instructions, the scoped docs, and the diff. The parent maps the change and gives no verdict.
4. After the parent session finishes, run every rule check as a **fork** of it. A fork is a new call that starts with the parent's full history, allowing the provider to reuse its prompt cache.
5. Load the previous correctness hypotheses for the branch. Generate new hypotheses when the diff or review instructions change. Verify the earlier and new hypotheses in batches. Each hypothesis that stays `UNSURE` gets another fork with that row and the reason it stayed open.
6. Print the summary, write the JSON report when asked, and exit.

Every agent can only read. The API backends get `read_file`, `grep` (git grep), and `list_dir`, all confined to the repo; `claude` gets `Read`, `Grep`, `Glob`; `codex` runs with `-s read-only`.

The backend that runs the parent session and the forks is one of the agents in [agent-plugins.md](agent-plugins.md).

See [review results](review-results.md) for verdicts, failure policy, usage, and the JSON report.

## Hypothesis cache

The cache scopes questions to a Git repository, branch, review base, generator prompt, and author context. Each question has a stable slug and the HEAD commit when the generator first wrote it. The report records the commit reviewed in the current run.

An unchanged diff skips the generator. After a commit, the generator focuses on the changes since the last cached review. A new merge base, a changed dirty snapshot, or a detached HEAD uses the full diff.

The cache stores questions in `~/.reviewrules/cache` and removes the least recently used entries first when it needs space. Every run verifies the questions against the current diff.

# How a run works

A run of reviewrules does these steps in this order:

1. Load the rules and the docs from the rule folders. Drop each rule whose `scope` matches no changed file.
2. Diff `merge-base(--base, HEAD)` against `HEAD`, or against the working tree with `--worktree`. Paths in `ignorePatterns` are left out.
3. Run the **parent session** once. Its first message holds the instructions, the scoped docs, and the diff. The parent maps the change and gives no verdict.
4. After the parent session finishes, run every rule check as a **fork** of it. A fork is a new call that starts with the parent's full history, allowing the provider to reuse its prompt cache.
5. Load the previous correctness hypotheses for the branch. Generate new hypotheses when the diff or review instructions change. Three generator forks run in parallel, one per hunt group: data (boundaries, absent values), failure (error paths, callers, pairs), and interleaving (concurrency, ownership, misuse). Each fork has its own cap, so a question in one group does not end the walk of another. Verify the earlier and new hypotheses in batches. Each hypothesis that stays `UNSURE` gets another fork with that row and the reason it stayed open.
6. Print the summary, write the JSON report when asked, and exit.

Every agent can only read. The API backends get `read_file`, `grep` (git grep), and `list_dir`, all confined to the repo; `claude` gets `Read`, `Grep`, `Glob`; `codex` runs with `-s read-only`.

The backend that runs the parent session and the forks is one of the agents in [agent-plugins.md](agent-plugins.md).

See [review results](review-results.md) for verdicts, failure policy, usage, and the JSON report.

## Hypothesis cache

The cache scopes questions to a Git repository, branch, and review base. Each question has a stable slug and the HEAD commit when the generator first wrote it. The report records the commit reviewed in the current run.

Unchanged review inputs skip the generator. A new `--context` note does not count as a change: the notes answer findings and leave the questions valid. After a commit, the generator focuses on the changes since the last cached review. When one hunt group fails, the run checks the questions of the other groups and keeps the cache as it was, so the next run walks the failed group again. If a worktree review changes, the generator uses the full diff already captured for that run.

The disk cache removes the least recently used entries first when it needs space. Every run verifies the questions against the current diff.

If a branch's questions exceed the cache budget, the cache drops that branch's entry. The next run generates from the full diff.

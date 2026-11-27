# How a run works

A run of reviewrules does these steps in this order:

1. Load the rules and the docs from the rule folders. Drop each rule whose `scope` matches no changed file.
2. Diff `merge-base(--base, HEAD)` against `HEAD`, or against the working tree with `--worktree`. Paths in `ignorePatterns` are left out.
3. Run the **parent session** once. Its first message holds the instructions, the scoped docs, and the diff. The parent maps the change and gives no verdict.
4. After the parent session finishes, run every rule check as a **fork** of it. A fork is a new call that starts with the parent's full history, allowing the provider to reuse its prompt cache.
5. Load the previous correctness hypotheses for the branch. Generate new hypotheses when the diff or review instructions change. Three generator forks run in parallel, one per hunt group: data (boundaries, absent values), failure (error paths, callers, pairs), and interleaving (concurrency, ownership, misuse). Each fork has its own cap, so a question in one group does not end the walk of another. Reuse the cached verdict of each hypothesis whose inputs did not change, and verify the other hypotheses in batches. Each hypothesis that stays `UNSURE` gets another fork with that row and the reason it stayed open.
6. Print the summary, write the JSON report when asked, and exit.

Every agent can only read. The API backends get `read_file`, `grep` (git grep), and `list_dir`, all confined to the repo; `claude` gets `Read`, `Grep`, `Glob`; `codex` runs with `-s read-only`.

The backend that runs the parent session and the forks is one of the agents in [agent-plugins.md](agent-plugins.md).

See [review results](review-results.md) for verdicts, failure policy, usage, and the JSON report.

## Hypothesis cache

The cache scopes questions and their verdicts to a Git repository, branch, and review base. Each question has a stable slug and the HEAD commit when the generator first wrote it. The report records the commit reviewed in the current run.

Unchanged review inputs skip the generator. A new `--context` note does not count as a change: the notes answer findings and leave the questions valid. A removed or changed note does count, because the questions written under it can leave out what the note claimed; so to take back a note, remove or change it rather than add a correction. After a commit, the generator focuses on the changes since the last cached review. When one hunt group fails, the run checks the questions of the other groups and keeps the cache as it was, so the next run walks the failed group again. If a worktree review changes, the generator uses the full diff already captured for that run.

## Verdict cache

The cache also keeps the last verdict of each question, with the HEAD commit of the run that judged it and the `--context` notes of that run. A run reuses a cached verdict and starts no checker fork for it when all of these hold:

- The checker model, reasoning effort, max steps, backend, check prompt, and review instructions are the same.
- The question has an `at`.
- No changed file is cited in `at` or in an evidence line. A changed file is one that differs between the judged commit and the current HEAD. A text cites it when the text holds the base name of the file followed by `:`, for example `guard.go:` for `src/guard.go`. So every spelling of the path counts: `./`, `../`, an absolute path, or backslashes. Another file with the same base name, or a text that only looks like a citation, costs one extra check.
- Every note of the judged run is still given.
- No note was added since, or the verdict is `REFUTED`. A new note can answer a `CONFIRMED` or `UNSURE` finding, so those are checked again.

A run checks the question again when it cannot tell: the judged commit is gone, or git fails. A check that ends in an error is not cached.

A `--worktree` run checks every question and stores no verdict, because its checkers read files that no commit holds. It keeps the verdicts already in the cache for the next run without `--worktree`.

The summary marks a reused verdict with `reused from <commit>`, and the JSON report sets `reused_from`. A reused verdict has no usage.

The run writes the questions and the verdicts in one cache write, after the checks.

## Cache size

The disk cache removes the least recently used entries first when it needs space.

If a branch's questions exceed the cache budget, the cache drops that branch's entry. The next run generates from the full diff.

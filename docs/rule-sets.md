# Rule sets

A rule set is a git repo that holds review rules for many repos. A repo adds a rule set with one `[[rule_sets]]` entry in its `.reviewrules/settings.toml`. The fields, the fetch, and the cache are in [configuration.md](configuration.md#rule-sets).

## Layout

```text
go-rules/
├── .reviewrules/
│   ├── config.json      # required: instructions and rules
│   └── files.json       # optional: docs that the rules refer to
└── docs/
    └── testing.md       # a doc that files.json names
```

- reviewrules reads only the root `.reviewrules/config.json` and `.reviewrules/files.json`. It does not read nested rule folders, `.reviewrules/settings.toml`, or `rule_folders` of the rule set.
- `config.json` and `files.json` have the same format as in a reviewed repo.
- `ignorePatterns` of the rule set is not used. Only the reviewed repo decides which files the diff ignores.

An example `config.json`:

```json
{
  "instructions": "The repo is Go code. Read the Go style doc before you check a rule.",
  "rules": [
    {"id": "test-conventions", "rule": "Tests: use table tests; use t.Context(); do not sleep", "scope": ["**/*_test.go"], "severity": "medium"},
    {"id": "wrap-errors", "rule": "Wrap a returned error with fmt.Errorf and %w.", "scope": ["**/*.go"]}
  ]
}
```

An example `files.json`:

```json
{
  "files": [
    {"path": "docs/testing.md", "description": "Go test conventions", "scope": ["**/*_test.go"]}
  ]
}
```

## Rule ids

Each rule id gets the prefix `<rule set name>/`. With `name = "go"`, the rule `test-conventions` is `go/test-conventions` in the summary and in the JSON report. When reviewrules splits a checklist rule into one check per item, each item keeps the prefix: `go/test-conventions#1`, `go/test-conventions#2`. Two rule sets can have rules with the same id, because their names are different.

## Scope globs

The `scope` globs of a rule and of a doc match the paths of the changed files in the reviewed repo, not the paths in the rule set repo. Write them from the root of the reviewed repo, for example `**/*.go`. A rule whose scope matches no changed file is skipped, as for the repo's own rules.

## Docs

The `path` of a doc in `files.json` is relative to the root of the rule set repo. reviewrules reads the doc from the rule set checkout and puts its text into the parent prompt, under the header `<path> (rule set <name>)`. A path that goes outside the rule set repo is an error. The agents can read only the reviewed repo, so a rule MUST NOT tell them to open a rule set file.

## Instructions

The `instructions` of the rule set go into the parent prompt after the instructions of the reviewed repo, under the header `# Rule set <name> review instructions`. The rule sets come in the order of the `rule_sets` list. Write instructions that apply to every repo that uses the rule set.

## Versions

Tag each release of the rule set, for example `v1.4.0`, and set `ref` to the tag. A team then changes `ref` in its own repo when it is ready for the new rules.

- A tag or a branch is fetched on each run. A branch gives each run the newest rules, so the result of a review can change with no change in the reviewed repo.
- A full 40-character commit sha never moves. When the cache has its checkout, the run uses no network.
- When a fetch fails, the run uses the last cached checkout of the same `ref` and logs a warning.

## Test a rule set on a repo

Use a `file://` URL to test a rule set before you push it.

1. Commit the rule set in its local repo, for example `/home/me/go-rules`. reviewrules fetches a commit, so changes that you did not commit are not used.
2. In the repo under review, add the entry to `.reviewrules/settings.toml`:

   ```toml
   [[rule_sets]]
   name = "go"
   url = "file:///home/me/go-rules"
   ref = "main"
   ```

3. Run `reviewrules --print-config` to check the entry, then run `reviewrules` as usual. The summary shows the rules of the set as `go/<rule id>`.
4. After each change to the rule set, commit it and run reviewrules again. A branch `ref` is fetched on each run.
5. Before you commit the reviewed repo, set `url` to the shared URL of the rule set and `ref` to a tag.

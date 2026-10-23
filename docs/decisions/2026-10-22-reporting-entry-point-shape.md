# Decision: shape of the reporting entry points

- Date: 2026-10-22
- Status: accepted
- Epic: 35.22 (CLI surface convention), task T6

## Context

atcr has seven commands that render reports over stored results:
`report`, `history`, `leaderboard`, `scorecard`, `quality-report`,
`audit-report`, and `debt dashboard`. The first six are top-level leaves and
`debt dashboard` is a subcommand of `debt`. In `atcr --help` they sat in one
alphabetical list with every other command, so nothing showed that they belong
together.

We considered two shapes.

## Rejected: a `report` command group

Move the entry points under one parent, for example `atcr report history` and
`atcr report scorecard`. We rejected this shape for two reasons:

1. `report [id-or-path]` is already a leaf that takes a positional argument
   (`cli/report.go`). If `report` also became a group, `atcr report history`
   would be ambiguous: is `history` a subcommand or a review id? The repo already
   fixed that leaf-and-group problem once, when the scanner moved from
   `atcr verify diff` to `atcr diff-smell` (`cli/verify_diff.go`).
2. Cobra aliases do not cross nesting levels. An alias on `report history`
   cannot answer to the old top-level `atcr history`. Keeping the old names
   working would need six permanent hidden top-level shim commands, and every
   doc, skill and CI hook would still have to move to the new names.

## Chosen: cobra help groups

Keep every command where it is and group them in help only, with cobra's
`AddGroup` and `GroupID` (in the pinned cobra v1.10.2). `atcr --help` now
lists the six top-level reporting commands under one heading:

```
Reporting Commands (also: atcr debt dashboard):
  audit-report   ...
  history        ...
  leaderboard    ...
  quality-report ...
  report         ...
  scorecard      ...
```

Cobra groups only a command's direct children, so `debt dashboard` cannot sit
in a root group without moving it out of `debt`, which would be a rename. The
group heading names it instead.

Why this shape:

- No command is renamed, so every existing command name, script and doc keeps
  working, and no alias shims are needed.
- `report [id-or-path]` stays a plain leaf.
- Grouping changes only how help is printed. `Commands()` returns the same
  tree, so the docs audit and the `--json` registry test are unaffected.

Implementation note: `AddGroup` runs before any `GroupID` is set, because cobra
panics when a command names a group that does not exist (`cli/main.go`).

## Consequences

- The tests in `cli/main_test.go` pin the heading, its members, and that every
  old command path still resolves and runs.
- A new reporting command joins the group by adding its name to
  `reportingCommands` in `cli/main.go`.

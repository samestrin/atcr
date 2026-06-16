---
id: mem-2026-06-16-b9cc9a
question: "Is a blank line between package declaration and import block in Go a valid TD item to remove?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/report/render.go]
tags: [clarifications, epic-3.5_severity-rank-consolidation, process, Go, gofmt, false-positive]
retrievals: 0
status: active
type: clarifications skill, 2026-06-16
---

# Is a blank line between package declaration and import block

## Decision

No — this is always a false positive. gofmt mandates exactly one blank line between the package clause and the import block when both are present. Any attempt to remove it will be immediately reinserted by gofmt on the next format pass or pre-commit hook run. The reviewer pattern-matched the blank line without distinguishing gofmt-required separators from truly unnecessary blank lines. When a TD row's FILE_LINE points to a package declaration and its Fix says "Remove blank line", verify whether the next non-blank line opens an import block — if it does, the fix is permanently unfixable and the row is a false positive that should be deleted from the TD table.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/render.go

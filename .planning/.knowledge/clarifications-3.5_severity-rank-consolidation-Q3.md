---
id: mem-2026-06-16-644345
question: "Is a blank line between package declaration and import block in a Go test file a valid TD item to remove?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/report/render_test.go]
tags: [clarifications, epic-3.5_severity-rank-consolidation, process, Go, gofmt, false-positive, test-files]
retrievals: 0
status: active
type: clarifications skill, 2026-06-16
---

# Is a blank line between package declaration and import block

## Decision

No — same rule as production Go files. gofmt enforces exactly one blank line between the package clause and the import block in all Go files, including _test.go files. Any TD row flagging this pattern as "Unnecessary blank line after package declaration" with Fix "Remove blank line" is a false positive when the next non-blank line opens an import block. The pre-commit hook (gofmt) will revert the change immediately. Delete the row from the TD table rather than attempting the fix.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/report/render_test.go

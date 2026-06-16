---
id: mem-2026-06-16-bef9f8
question: "How to determine whether an out-of-scope comment should be deferred to a new Epic Plan or closed as accepted enhancement debt?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/mcp/handlers.go]
tags: [clarifications, epic-3.4_scorecard-diagnostics-writer, scope, process, enhancement-debt, resolve-td]
retrievals: 0
status: active
type: clarifications /execute-epic 2026-06-16
---

# How to determine whether an out-of-scope comment should be d

## Decision

Check two things before deciding: (1) Is the code actually deferred/conditional, or is the "deferred" language only in a comment? If the implementation is unconditional and working, the comment is documentation of a deliberate design decision, not a stub. (2) Are all ACs already satisfied [x]? If yes, the epic contract is fully met and there is nothing substantive to defer — only an optional future enhancement remains. In that case, close as accepted enhancement debt. Creating a new Epic Plan for an out-of-scope comment on a fully-satisfied epic is speculative gold-plating. The comment itself (documenting what was deliberately excluded) satisfies the AC requiring a "documented default." Example: `internal/mcp/handlers.go:214–219` documents the e.log exclusion and satisfies AC4 of epic 3.4.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/mcp/handlers.go

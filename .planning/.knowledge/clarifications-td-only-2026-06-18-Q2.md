---
id: mem-2026-06-18-d6ba4e
question: "When a resolve-td group scope excludes a file required by a cross-file refactor, should you expand the group or re-run without --group?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [internal/fanout/resume.go, internal/fanout/review.go]
tags: [td-clarification, td-only, process, resolve-td, group-scope, refactoring, maintainability]
retrievals: 0
status: active
type: clarifications/td-only/2026-06-18
---

# When a resolve-td group scope excludes a file required by a 

## Decision

Re-run without --group (or with a new group scoped to both files) rather than expanding an existing group's scope mid-pass. Expanding an existing group risks scope creep and can conflict with other in-progress group items. A cross-file refactor (e.g. deduplicating two near-identical functions across resume.go and review.go by introducing a shared interface) needs both files in scope at once — if the current group doesn't cover both, the cleanest path is a standalone ungrouped pass targeting only that item. The group assignment is a routing decision, not a technical constraint; re-routing is always safe.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/resume.go
- internal/fanout/review.go

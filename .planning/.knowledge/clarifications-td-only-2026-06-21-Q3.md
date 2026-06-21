---
id: mem-2026-06-21-a85209
question: "Should the duplicate \"File not found\" format string in internal/reconcile/emit.go and internal/report/render.go be extracted into a shared constant or helper?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, internal/report/render.go]
tags: [td-clarification, td-only, cross-cutting, architecture, duplication, reconcile, report]
retrievals: 0
status: active
type: clarifications skill, td-only mode, 2026-06-21
---

# Should the duplicate "File not found" format string in inter

## Decision

No — close as won't-fix. The project's recorded architectural position (captured in the TD row's own Fix column) is that a cross-package dependency is not justified for one format string. The two occurrences (internal/reconcile/emit.go:341 and internal/report/render.go:326) operate on distinct types in distinct packages with no other shared dependency. Extracting a shared constant would require either a new bridging package or a one-way import for negligible benefit. Revisit only if a common low-level rendering package emerges that already bridges the two packages.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- internal/report/render.go

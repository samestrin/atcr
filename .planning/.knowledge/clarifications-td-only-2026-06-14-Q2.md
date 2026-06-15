---
id: mem-2026-06-14-48942a
question: "Should atcr verify re-emit disagreements.json to include verification_disagreement entries?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/reconcile/disagree.go, internal/reconcile/emit.go]
tags: [td-clarification, td-only, architecture, CONTRACT, disagree-radar, verify-pipeline]
retrievals: 0
status: active
type: td-clarification
---

# Should atcr verify re-emit disagreements.json to include ver

## Decision

No — disagreements.json is intentionally a reconcile-time-only artifact. It carries only reconcile-time tension classes (severity splits, solo findings, gray-zone clusters). The verification_disagreement class is surfaced only by the live radar (atcr report), not by this snapshot. This design is documented in the DisagreementsFile godoc at internal/reconcile/disagree.go:73-79. Epic 3.2 Task 5 scoped disagreements.json to reconcile time; Task 4 scoped verification intake to the live radar. Re-emitting from the verify stage would expand the output contract beyond the intentional split.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/disagree.go
- internal/reconcile/emit.go

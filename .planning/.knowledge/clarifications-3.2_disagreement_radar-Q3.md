---
id: mem-2026-06-14-91790a
question: "Does persona tension (correctness vs design reviewer) have data model support in the codebase, and is it in scope for the disagreement radar?"
created: 2026-06-14
last_retrieved: ""
sprints: []
files: [internal/stream/parser.go, internal/reconcile/merge.go]
tags: [clarifications, epic-3.2_disagreement_radar, scope, persona-tension, data-model]
retrievals: 0
status: active
type: clarifications /3.2_disagreement_radar.md
---

# Does persona tension (correctness vs design reviewer) have d

## Decision

Persona tension is out of scope and has no data model support. Finding.Reviewer and Finding.Reviewers store plain model-name strings only (internal/stream/parser.go:46-59); there is no persona or role field anywhere in the schema. A codebase-wide grep for persona|role in /internal returns zero matches. The radar surfaces exactly four input types: severity splits, solo findings, gray-zone clusters, and verification disagreements. Persona tension appears only in narrative prose under "What the radar surfaces" — it is absent from both the Acceptance Criteria and the Task Breakdown, which are the binding contract. Surfacing persona tension would require new data plumbing this epic does not build.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/stream/parser.go
- internal/reconcile/merge.go

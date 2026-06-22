---
id: mem-2026-06-21-c1da88
question: "clusterDisplayProblem vs reconcile.longestProblem: unify into one exported function or pin coupling with a round-trip regression test?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/debate/cluster.go, internal/reconcile/disagree.go]
tags: [td-clarification, td-only, maintainability, scope, testing, debate, reconcile, gray-zone, cluster]
retrievals: 0
status: active
type: clarifications skill 2026-06-21
---

# clusterDisplayProblem vs reconcile.longestProblem: unify int

## Decision

Use the round-trip regression test. Both functions are byte-identical (strict greater-than longest-problem, first-wins ties). Unifying requires exporting reconcile.longestProblem and crossing the debate→reconcile package boundary — outside group-2 scope per the Epic 6.1 design that keeps these packages loosely coupled. The test round-trips a cluster through BuildDisagreements → indexClusters lookup, including the equal-length-problem tie case, and pins the coupling without modifying either production function. Needs manual approval past the over-simplification gate (test-only change). JUSTIFICATION: internal/debate/cluster.go:16-24 (clusterDisplayProblem) and internal/reconcile/disagree.go (longestProblem) are logically identical; grayZoneItem calls longestProblem to set it.Problem which must match indexClusters' key built from clusterDisplayProblem(c). Epic 6.1 locked a narrow in-memory approach keeping debate and reconcile loosely coupled.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/debate/cluster.go
- internal/reconcile/disagree.go

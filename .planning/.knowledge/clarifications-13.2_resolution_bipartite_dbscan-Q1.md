---
id: mem-2026-06-28-38bd6b
question: "What concrete bound or guard should be added for the Hungarian algorithm loop index in reconcile/bipartite.go?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/bipartite.go, reconcile/dedupe.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, implementation, hungarian-algorithm, bipartite-matching, size-guard]
retrievals: 0
status: active
type: clarifications epic 13.2_resolution_bipartite_dbscan 2026-06-28
---

# What concrete bound or guard should be added for the Hungari

## Decision

Keep `iter` as `int` (already the case). Add a maximum matrix-size guard at the entry of `hungarian()` — before any allocations — that panics if `n > 500`. This is 10× the epic's stated O(V³)-safe bound of V < 50, rejects clearly degenerate inputs, and keeps plain `int` throughout so slice indexing stays valid. Place the guard at reconcile/bipartite.go after the `n == 0` early-return. The architectural bound V < 50 comes from bipartite matching operating within an existing location/AST cluster; a matrix larger than ~50 indicates a broken upstream grouper. A limit of 500 gives a 10× engineering margin while keeping O(V³) ≈ 1.25×10⁸ operations tractable.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/bipartite.go
- reconcile/dedupe.go

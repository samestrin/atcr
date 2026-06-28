---
id: mem-2026-06-28-be7d4b
question: "Should the DBSCAN noise ID use the cluster index (unstable) or a stable content-addressed identifier? What about two identical same-source findings producing the same ID?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/dedupe.go, reconcile/ambiguous.go, internal/debate/cluster.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, implementation, dbscan, noise-id, source-keys, collision]
retrievals: 0
status: active
type: clarifications epic 13.2_resolution_bipartite_dbscan 2026-06-28
---

# Should the DBSCAN noise ID use the cluster index (unstable) 

## Decision

Use the content-addressed identifier `AmbiguousID(file, line, problem, problem)` — never the cluster index. The same-source ID collision concern is structurally prevented: the `denseNeighbor` predicate at reconcile/dedupe.go:204 requires `srcKeys[a] != srcKeys[b]`, so two findings from the same source cannot both appear as DBSCAN noise in the same location cluster. In the theoretical edge case where both still land as noise, they produce the same ID — which is harmless because `internal/debate` skips both via `len(c.Findings) < 2` (cluster.go:136). Cluster index is ruled out because it is unstable across reordering, violating the cross-run stability requirement of `AmbiguousCluster.ID`.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/ambiguous.go
- internal/debate/cluster.go

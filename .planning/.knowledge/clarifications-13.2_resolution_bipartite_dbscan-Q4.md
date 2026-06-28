---
id: mem-2026-06-28-bd155b
question: "Does the AmbiguousCluster schema and internal/debate consumer need to break when DBSCAN introduces singleton noise findings into ambiguous.json?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/dedupe.go, internal/debate/cluster.go, reconcile/ambiguous.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, architecture, scope, AmbiguousCluster, DBSCAN, internal/debate, backward-compatibility]
retrievals: 0
status: active
type: clarifications skill — epic 13.2_resolution_bipartite_dbscan, 2026-06-28
---

# Does the AmbiguousCluster schema and internal/debate consume

## Decision

No breaking change required for the wire shape or debate consumer. The AmbiguousCluster struct (reconcile/dedupe.go:27) uses `Findings []Finding` with no "exactly 2" type constraint — the pair assumption is only in the production dedupeCluster code path, not the type. internal/debate/cluster.go:136 already has an explicit guard `if len(c.Findings) < 2 { skipped++; continue }`, so DBSCAN singletons represented as AmbiguousCluster{Findings:[one_finding]} are gracefully skipped by the debate consumer (no panic, no merge attempted). Golden-corpus fixtures and AmbiguousHash must be rebaselined (any pipeline rewrite changes the output), but ambiguous.json's wire schema and internal/debate can stay backward-compatible by representing DBSCAN noise as len=1 AmbiguousCluster entries with no Similarity field.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- internal/debate/cluster.go
- reconcile/ambiguous.go

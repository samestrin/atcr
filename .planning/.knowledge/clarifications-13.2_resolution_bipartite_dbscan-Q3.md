---
id: mem-2026-06-28-d5b5f9
question: "Should DBSCAN noise entries use a separate \"noise-\" ID prefix or remain inside the \"amb-\" namespace?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/dedupe.go, reconcile/ambiguous.go, internal/debate/cluster.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, architecture, dbscan, ambiguous-id, wire-shape]
retrievals: 0
status: active
type: clarifications epic 13.2_resolution_bipartite_dbscan 2026-06-28
---

# Should DBSCAN noise entries use a separate "noise-" ID prefi

## Decision

Noise entries must remain inside the "amb-" namespace. The implementation already does this — dedupe.go:218 calls AmbiguousID() for noise entries, which always returns "amb-" + hex (reconcile/ambiguous.go:33). Adding a "noise-" prefix would change the ambiguous.json wire shape (forbidden), and provides no functional benefit: internal/debate distinguishes noise by `len(c.Findings) < 2` (cluster.go:136), treating the ID as an opaque equality key without inspecting the prefix. The schema must not acquire a second prefix token — that is a wire-shape break even if the consumer happens to ignore it.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/ambiguous.go
- internal/debate/cluster.go

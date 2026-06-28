---
id: mem-2026-06-28-a8f2ea
question: "How should the stable ID in reconcile/dedupe.go preserve both cross-run stability and per-finding uniqueness for anonymous source keys?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/dedupe.go, reconcile/ambiguous.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, architecture, ambiguous-id, source-keys, cross-run-stability]
retrievals: 0
status: active
type: clarifications epic 13.2_resolution_bipartite_dbscan 2026-06-28
---

# How should the stable ID in reconcile/dedupe.go preserve bot

## Decision

No change needed. The `srcKeys` at reconcile/dedupe.go:89-95 are run-local bipartite matching handles — their only job is to ensure each unattributed finding is treated as its own source within a single reconcile pass. Cross-run stability is a separate, orthogonal concern handled by `AmbiguousID` (reconcile/ambiguous.go:18), a content-addressed SHA-256 over (file, line, sorted problem pair) that is the durable cross-run handle recorded in ambiguous.json. The two concerns are intentionally split: `srcKeys` for intra-run uniqueness, `AmbiguousID` for cross-run stability. Conflating them would add complexity with no benefit.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/ambiguous.go

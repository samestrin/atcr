---
id: mem-2026-06-28-dca906
question: "Should empty or whitespace-only problem texts be treated as \"no signal\" (relDistinct) in the token-set Jaccard distance, or is the current merge behavior intentional?"
created: 2026-06-28
last_retrieved: ""
sprints: []
files: [reconcile/dedupe.go, reconcile/dedupe_test.go]
tags: [clarifications, epic-13.2_resolution_bipartite_dbscan, implementation, jaccard, classify, empty-tokens, behavioral-contract]
retrievals: 0
status: active
type: clarifications epic 13.2_resolution_bipartite_dbscan 2026-06-28
---

# Should empty or whitespace-only problem texts be treated as 

## Decision

The merge behavior is intentional and must be preserved. `classify()` has an explicit comment-documented rule at reconcile/dedupe.go:256-260: two empty token sets are identical (relMerge, 1.0). A whitespace-only string also produces an empty token set via `tokenize()` (which filters empty tokens at dedupe.go:295-302), so it is treated identically to a truly empty string. The behavioral contract is pinned by `TestDedupeCluster_BothEmptyProblemsMerge` (reconcile/dedupe_test.go:109-116). Any new or rewritten edge-weight formula (e.g., the bipartite matching distance function) must assign `distance = 0` when both token sets are empty — not `relDistinct`.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/dedupe_test.go

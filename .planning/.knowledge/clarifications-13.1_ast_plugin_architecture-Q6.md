---
id: mem-2026-06-27-826d16
question: "Should a code cap bound keyed cluster size in ClusterWith to prevent O(n²) deduplication, and what is the fallback on overflow?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/reconcile/grouper.go, internal/reconcile/cluster.go, internal/reconcile/dedupe.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, architecture, performance, clustering, grouper, keyed-cluster, proximity-fallback]
retrievals: 0
status: active
type: clarifications
---

# Should a code cap bound keyed cluster size in ClusterWith to

## Decision

No code cap is warranted. A proximity fallback for an oversized keyed cluster is architecturally incoherent: the Grouper contract states same-key findings cluster "regardless of line distance" (reconcile/grouper.go:12-15), so a proximity fallback would silently violate that invariant — a finding pair such as lines 10 and 40 sharing a key would NOT cluster under ±3 proximity, and there is no coherent split strategy for which findings to demote. Three additional reasons to reject a code cap: (1) no proximity cap exists in proximityClusters (cluster.go:77-96), so adding one only to ClusterWith would be inconsistent; (2) the codebase already accepts the O(n²) cost in a comment at reconcile/dedupe.go:49 ("stays cheap" — token set intersections are pre-computed per finding, not per pair); (3) the epic's only performance NFR is the <10ms Wasm-instantiation bound, not the dedupe step. If a future hotspot is confirmed empirically, the correct remedy is a hard-abort or log+skip, not a proximity fallback. The correct action is an inline comment at reconcile/grouper.go:64-68 documenting this design decision.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/grouper.go
- internal/reconcile/cluster.go
- internal/reconcile/dedupe.go

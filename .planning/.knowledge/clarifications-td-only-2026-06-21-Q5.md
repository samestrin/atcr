---
id: mem-2026-06-21-43d7da
question: "Should the filterMergedClusters co-located-distinct-cluster over-suppression limitation be fixed inline, closed as accepted, or deferred to a new Epic Plan?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/debate/cluster.go, internal/reconcile/emit.go]
tags: [td-clarification, td-only, scope, cluster-id, epic-6.1, filterMergedClusters, JSONFinding]
retrievals: 0
status: active
type: td-clarification
---

# Should the filterMergedClusters co-located-distinct-cluster 

## Decision

Defer to new Epic Plan 6.2 (cluster-id-on-finding). The limitation is consciously accepted at LOW/self-healing severity — the code comment at cluster.go:50-68 documents it explicitly and TestFilterMergedClusters_CoLocatedDistinctClustersOverSuppressed pins the behavior. The fix requires adding a ClusterID field to JSONFinding (emit.go struct + JSONFindings() converter + applyOneClusterMerge setter + filterMergedClusters reader + findings.json schema change), a four-file structural extension across two packages that exceeds the TD quick-fix boundary. The scenario (two distinct gray-zone clusters at identical canonical File+Line simultaneously) is rare; the second cluster resurfaces on the next fresh reconcile. The natural vehicle for the fix is co-scheduling with Epic 13.2 (DBSCAN), where stable cluster IDs would emerge organically from the clustering redesign.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/debate/cluster.go
- internal/reconcile/emit.go

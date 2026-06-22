---
id: mem-2026-06-21-239b41
question: "filterMergedClusters location-key over-suppression: accept co-located cluster limitation or implement cluster-identity tracking?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/debate/cluster.go]
tags: [td-clarification, td-only, correctness, scope, testing, debate, gray-zone, cluster, idempotency]
retrievals: 0
status: active
type: clarifications skill 2026-06-21
---

# filterMergedClusters location-key over-suppression: accept c

## Decision

Accept and document the limitation. filterMergedClusters at internal/debate/cluster.go:50 keys solely on locationKey(File,Line); two distinct gray-zone clusters at the same canonical File+Line will have cluster #2 suppressed pre-debate after cluster #1 is merged. The condition is self-healing (cluster #2 re-debates on the next fresh reconcile, no data corruption). Cluster-identity tracking requires adding a field to reconcile.JSONFinding and reconcile.AmbiguousCluster — a cross-package schema change outside group-2 scope. The correct full fix is already tracked as a separate LOW item in the epic-6.1 section. Group-2 action: add a comment documenting the co-location limitation + a test with two co-located clusters pinning the known behavior. NOTE: indexClusters uses a 3-field FindingKey (File+Line+Problem) so it correctly distinguishes co-located clusters; the over-suppression is isolated to filterMergedClusters only.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/debate/cluster.go

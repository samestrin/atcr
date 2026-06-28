---
id: mem-2026-06-27-b38c4c
question: "Is ClusterWith's file-level grouping (grouper.go:46-49 putting all file-level findings into one cluster per file) a defect or working-as-designed?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/reconcile/grouper.go, internal/reconcile/cluster.go]
tags: [clarifications, epic-13.1_ast_plugin_architecture, false-positive, clustering, file-level-findings, two-phase-design]
retrievals: 0
status: active
type: clarifications
---

# Is ClusterWith's file-level grouping (grouper.go:46-49 putti

## Decision

Working-as-designed — close as false positive. reconcile/grouper.go:46-49 is semantically identical to the baseline Cluster at reconcile/cluster.go:37-39. The behavior is explicitly documented as intentional at cluster.go:21-22: "File-level findings (Line <= 0) form one cluster per file, kept separate from line-specific clusters." Grouping all file-level findings into one cluster per file only scopes which findings are compared; actual merging is gated by PROBLEM-text similarity in DedupeCluster (cluster.go:16-19), so a loose cluster never over-collapses dissimilar findings. A reviewer unfamiliar with the two-phase design (cluster-to-scope → dedupe-to-merge) would plausibly conflate "all in one cluster" with "all merged together" — that conflation is the likely source of the false finding. File-level findings also never enter the AST key lookup loop at grouper.go:55-65 (they are separated out by splitFileLevel at line 46 before the key loop runs), so the AST path cannot change their treatment.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/grouper.go
- internal/reconcile/cluster.go

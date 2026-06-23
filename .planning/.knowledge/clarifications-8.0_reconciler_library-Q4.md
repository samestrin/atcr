---
id: mem-2026-06-23-5a092e
question: "For the reconciler library extraction, which pieces are public API vs ATCR-internal? Is the Verification type cleanly ATCR-internal?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, internal/reconcile/merge.go, internal/reconcile/disagree.go, internal/reconcile/gate.go, internal/reconcile/cluster.go, internal/reconcile/validate.go]
tags: [clarifications, epic-8.0_reconciler_library, architecture, scope, api-boundary, verification]
retrievals: 0
status: active
type: clarifications
---

# For the reconciler library extraction, which pieces are publ

## Decision

gate.go, validate.go, and emit.go's I/O layer (Emit, RunReconcile, ReadReconciledFindings, renderers) correctly stay ATCR-internal. However, Verification / VerdictConfirmed/Refuted/Unverifiable / JSONFinding are NOT cleanly ATCR-internal: Verification is embedded in Merged (the output of Merge() at merge.go:63), mergeVerification() implements verdict-precedence logic inside the cluster-merge algorithm (merge.go:409-443), and BuildDisagreements reads f.Verification to classify radar items (disagree.go:132,211,225). These must either travel with the library (become public API surface) or be decoupled via an interface/opaque type before extraction. stream.Finding is also a forced dependency for Cluster(), Merge(), and DedupeCluster() — both entanglements must be resolved in Phase 0 before extraction can compile.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- internal/reconcile/merge.go
- internal/reconcile/disagree.go
- internal/reconcile/gate.go
- internal/reconcile/cluster.go
- internal/reconcile/validate.go

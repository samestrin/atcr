---
id: mem-2026-06-23-39ac8c
question: "Is AmbiguousCluster.Similarity on the dedup decision path, and is changing it from float64 to an integer ratio acceptable?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/dedupe.go, reconcile/ambiguous.go]
tags: [clarifications, sprint-8.0_reconciler_library, architecture, reconcile/dedupe.go, determinism]
retrievals: 0
status: active
type: clarifications /resolve-td 2026-06-23
---

# Is AmbiguousCluster.Similarity on the dedup decision path, a

## Decision

AmbiguousCluster.Similarity (float64, dedupe.go:31) is ADVISORY ONLY — for display and tests. The merge decision uses exclusively integer cross-multiply at dedupe.go:148–153 (inter*10 >= union*7, inter*10 >= union*4) and never reads sim. The guard comment at dedupe.go:143–146 documents this boundary explicitly. Changing the field type to an integer ratio would change the public API and break the ambiguous.json wire format, invalidating TestGoldenCorpus_ByteIdentical (a binding sprint contract). The schema change is deferred until a planned breaking version that retires golden fixtures. Correct resolution: extend the comment and close the TD item as comment-only.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/dedupe.go
- reconcile/ambiguous.go

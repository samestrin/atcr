---
id: mem-2026-06-23-dbcdff
question: "How does library Reconcile populate Summary.SkippedSources/SkippedSourceCount when library Source has no SkippedFiles (Sprint 8.0)?"
created: 2026-06-23
last_retrieved: ""
sprints: [8.0_reconciler_library]
files: [reconcile/source.go, internal/reconcile/discover.go, internal/reconcile/reconcile.go]
tags: [clarifications, sprint-8.0_reconciler_library, architecture, SkippedSources, boundary-adapter, Summary, Source]
retrievals: 0
status: active
type: clarifications
---

# How does library Reconcile populate Summary.SkippedSources/S

## Decision

It doesn't — the library always produces SkippedSources=[] and SkippedSourceCount=0. This is already implemented: reconcile/source.go (Phase 1) has no SkippedFiles field and an explicit comment confirming this is by design. ATCR's adapter or RunReconcile stamps the real skipped-source values onto Result.Summary after Reconcile returns. The library Summary type carries SkippedSources/SkippedSourceCount fields (lifted as-is from internal/reconcile), so ATCR can write to them post-call. summary.json is preserved byte-for-byte because the fields still exist in output — they are populated one layer up by ATCR. Phase 2 must implement the post-Reconcile stamp in the ATCR adapter, reading from ATCR-internal Source.SkippedFiles.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- reconcile/source.go
- internal/reconcile/discover.go
- internal/reconcile/reconcile.go

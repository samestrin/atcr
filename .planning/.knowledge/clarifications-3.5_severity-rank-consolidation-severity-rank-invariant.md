---
id: mem-2026-06-16-e36976
question: "Why is stream.SeverityRank left as a plain mutable var map rather than guarded by a Rank() accessor or immutability test?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/stream/severity.go, internal/reconcile/merge.go, internal/reconcile/merge_test.go]
tags: [clarifications, epic-3.5_severity-rank-consolidation, architecture, stream, SeverityRank, immutability, design-decision]
retrievals: 0
status: active
type: clarifications skill, 2026-06-16
---

# Why is stream.SeverityRank left as a plain mutable var map r

## Decision

Leave the comment-only invariant as-is. The "read-only after init" guarantee is already enforced structurally: reconcile's copy-on-init pattern (internal/reconcile/merge.go:29-31) copies stream.SeverityRank into a local snapshot before tests mutate it, so no consumer ever writes to stream.SeverityRank directly. Grep across all 13 consumer files confirms zero write assignments to stream.SeverityRank in production code. The only mutation is in internal/reconcile/merge_test.go:151 (`SeverityRank["CRITICAL"] = 999`), which targets the reconcile-local copy, not stream.SeverityRank. An immutability test trips the diff_smell over-simplification gate. A Rank() accessor would cascade to 14+ direct-lookup sites (fanout/postprocess.go:28/:38/:58, disagree.go ×8+, gate.go:47/:51, merge.go:119, reconcile.go:110, render.go:426, verify/severity.go:15-16), breaking the binding design decision that all 14 reconcile lookups compile unchanged via re-export alias. The comment at internal/stream/severity.go:18-21 documents the invariant and the mitigation pattern ("copy it locally first"), which is all that is needed for a pure-consolidation epic.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/stream/severity.go
- internal/reconcile/merge.go
- internal/reconcile/merge_test.go

---
id: mem-2026-06-23-07e864
question: "Should the reconciler library absorb internal/stream, or define its own Finding type and have ATCR adapt at the boundary?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/stream/fileindex.go, internal/stream/validate.go, internal/stream/severity.go, internal/stream/levenshtein.go, internal/reconcile/merge.go, internal/reconcile/cluster.go]
tags: [clarifications, epic-8.0_reconciler_library, architecture, stream-dependency, finding-type]
retrievals: 0
status: active
type: clarifications
---

# Should the reconciler library absorb internal/stream, or def

## Decision

Define a clean Finding type in the library; adapt at the boundary (option a). stream is not pure: fileindex.go and validate.go import internal/metrics (ATCR observability), so absorbing stream wholesale drags that dependency into the public library. The two genuinely portable pieces — severity.go (NormalizeSeverity + SeverityRank, stdlib-only) and levenshtein.go (pure algorithm) — should move INTO the library. The library's Finding type needs all 9 wire-format fields: Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence, Reviewer/Reviewers, Confidence. Path-validation fields (PathValid, PathWarning, PathSuggestion) stay in the ATCR adapter. merge.go:30-36 already defensively copies stream.SeverityRank into its own reconcile.SeverityRank, suggesting the package was designed to tolerate some separation.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/stream/fileindex.go
- internal/stream/validate.go
- internal/stream/severity.go
- internal/stream/levenshtein.go
- internal/reconcile/merge.go
- internal/reconcile/cluster.go

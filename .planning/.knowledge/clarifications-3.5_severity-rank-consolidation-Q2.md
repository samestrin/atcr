---
id: mem-2026-06-16-1f22a8
question: "Does the stream parser normalize Finding.Severity before findings reach the reconcile pipeline?"
created: 2026-06-16
last_retrieved: ""
sprints: []
files: [internal/stream/parser.go, internal/stream/severity.go, internal/reconcile/merge.go, internal/fanout/postprocess.go]
tags: [clarifications, epic-3.5_severity-rank-consolidation, architecture, parser, severity-normalization, stream]
retrievals: 0
status: active
type: clarifications
---

# Does the stream parser normalize Finding.Severity before fin

## Decision

No. The parser stores Finding.Severity verbatim from the raw TSV column (internal/stream/parser.go:193: `Severity: f[0]`). stream.NormalizeSeverity is defined in stream/severity.go:33 as strings.ToUpper(strings.TrimSpace(s)) but is NOT called by the parser — it is a utility that callers must invoke explicitly at their lookup boundaries. Findings therefore arrive at reconcile with whatever casing the LLM reviewer emitted. mergeSeverity() in internal/reconcile/merge.go is the first and only canonicalization point in the reconcile pipeline, which is why the epic's boundary fixes at merge.go:104 and disagree.go:338 are necessary and correct. postprocess.go:38 independently confirms the convention: it also calls stream.NormalizeSeverity before its own SeverityRank lookup.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/stream/parser.go
- internal/stream/severity.go
- internal/reconcile/merge.go
- internal/fanout/postprocess.go

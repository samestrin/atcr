---
id: mem-2026-06-19-cb2717
question: "What are the correct backup units for reconcile and verify stages in Epic 4.7 idempotency?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/reconcile/emit.go, internal/reconcile/gate.go, internal/verify/pipeline.go, internal/verify/emit_verification.go, internal/verify/emit_findings.go]
tags: [clarifications, epic-4.7_idempotency, architecture, implementation, reconcile, verify, backup]
retrievals: 0
status: active
type: clarifications
---

# What are the correct backup units for reconcile and verify s

## Decision

Reconcile owns the entire `reconciled/` directory (6 files: findings.txt, findings.json, report.md, summary.json, ambiguous.json, disagreements.json) and should back it up to `reconciled.bak/` before re-emitting. Verify's only exclusive output is `reconciled/verification.json`; back that file up to `reconciled/verification.json.bak` before re-writing it. The other files verify touches (findings.json, summary.json) are reconcile-owned artifacts annotated in-place — already covered by the reconciled.bak/ directory copy. Per-file .bak for those (Option B) creates redundant duplicates. Key evidence: internal/reconcile/emit.go:100-131 writes all 6 artifacts to reconciled/; internal/verify/pipeline.go:319-351 writes 4 files in one batch but only verification.json is verify-exclusive; internal/verify/emit_verification.go:144-150 writes only reconciled/verification.json.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/emit.go
- internal/reconcile/gate.go
- internal/verify/pipeline.go
- internal/verify/emit_verification.go
- internal/verify/emit_findings.go

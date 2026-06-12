---
id: mem-2026-06-12-9abe99
question: "Should FailureMarker bool be added to PoolSummary in artifacts.go to distinguish a WritePool failure summary from a real all-failed run, or deferred, or accepted as-is?"
created: 2026-06-12
last_retrieved: ""
sprints: []
files: [internal/fanout/artifacts.go, internal/fanout/outcome.go, internal/reconcile/gate.go, internal/reconcile/reconcile.go, internal/reconcile/discover.go]
tags: [td-clarification, td-only, architecture, fanout, reconcile]
retrievals: 0
status: active
type: td-clarification
---

# Should FailureMarker bool be added to PoolSummary in artifac

## Decision

Defer the full fix (option b). The TD row's premise is partially outdated: writeFailureSummary already uses summarize(results) (artifacts.go:110-116), not {Total: roster, Failed: roster} — the fabricated-all-failed concern is resolved by epic 1.5 (merge 343c20ce). The residual real bug is narrower: if WritePool fails while writing agent D's on-disk artifacts after D succeeded in-memory, summarize(results) counts D as Succeeded → failure summary has Partial:false → the caller threads opts.Partial:false to RunReconcile → reconcile produces a non-partial verdict covering only the agents whose findings.txt files exist on disk. A FailureMarker bool field alone (option a) is a no-op: RunReconcile in gate.go does not read pool/summary.json — the CALLER does (threaded as opts.Partial per reconcile.go:11-12). Adding the struct field without updating the caller adds speculative code. The fix requires two coordinated changes: (1) artifacts.go sets FailureMarker:true in writeFailureSummary, (2) the caller (report/skill layer) reads FailureMarker from pool/summary.json and forces opts.Partial:true. These must land together under Epic 1.9 fanout-writepool-failure-marker. Option (c) is defensible given the narrow failure scenario (write failure on a specific agent's artifacts after in-memory success), but the silent verdict gap is a real correctness issue.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/artifacts.go
- internal/fanout/outcome.go
- internal/reconcile/gate.go
- internal/reconcile/reconcile.go
- internal/reconcile/discover.go

---
id: mem-2026-06-16-619dd9
question: "Is disagreements.json verification-free by structural design, or only by pipeline ordering? Should Emit strip the verification tier?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/reconcile/disagree.go, internal/reconcile/emit.go, internal/reconcile/gate.go, internal/reconcile/merge.go, internal/verify/pipeline.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, reconcile, disagreements, verification, pipeline-ordering]
retrievals: 0
status: active
type: clarifications
---

# Is disagreements.json verification-free by structural design

## Decision

disagreements.json is verification-free by pipeline ordering, NOT by structural design — and that is the correct invariant to pin in tests; do not add a production strip. reconcile.Emit is the sole writer of disagreements.json (internal/reconcile/emit.go:104) and runs strictly before the verify stage (internal/reconcile/gate.go:229). At Emit time the Verification block is nil ("populated during the verify re-emit; nil for a v1 finding", internal/reconcile/merge.go:44-48). The verify stage reads reconciled findings and writes only verification.json — it never re-invokes reconcile.Emit (internal/verify/pipeline.go:105). The isVerificationTie branch (internal/reconcile/disagree.go:128) MUST stay in BuildDisagreements because the live atcr report radar path (cmd/atcr/report.go:81, internal/mcp/handlers.go:279) legitimately sees verification and needs it. Tradeoff: the invariant is enforced by call-ordering, not the type system; if structural protection is ever wanted, add an Emit-level precondition guard rather than stripping the tier from the shared BuildDisagreements.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/disagree.go
- internal/reconcile/emit.go
- internal/reconcile/gate.go
- internal/reconcile/merge.go
- internal/verify/pipeline.go

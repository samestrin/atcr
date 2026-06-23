---
id: mem-2026-06-23-d7f042
question: "When extracting the reconciler into a standalone library, should the existing internal API be lifted as-is or reshaped to the epic's proposed clean API?"
created: 2026-06-23
last_retrieved: ""
sprints: []
files: [internal/reconcile/reconcile.go, internal/reconcile/merge.go, internal/reconcile/gate.go, internal/reconcile/emit_test.go, internal/reconcile/gate_test.go]
tags: [clarifications, epic-8.0_reconciler_library, architecture, scope, api-design]
retrievals: 0
status: active
type: clarifications
---

# When extracting the reconciler into a standalone library, sh

## Decision

Lift the existing API as-is (option a). The epic's proposed clean API (Reconcile returning *Result, error; Options{LineTolerance, SimilarityThreshold}; ReconciledFinding) is incompatible with the working code: the real signature is Reconcile(sources, opts) Result (value return, no error), Options carries {ReconciledAt, Partial, Merges, Root}, and output type is Merged (embeds stream.Finding + *Verification). There are 14+ test call sites using Options.ReconciledAt and asserting on Merged/Verification — all would break under the clean API. Lift as-is first (AC#3 satisfied on day one); do the clean-API reshaping as a follow-on epic once the extraction boundary is stable. The epic's own reconciliation note acknowledged: "The current plan assumes Result can be lifted as-is; it cannot without untangling these first."

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/reconcile.go
- internal/reconcile/merge.go
- internal/reconcile/gate.go
- internal/reconcile/emit_test.go
- internal/reconcile/gate_test.go

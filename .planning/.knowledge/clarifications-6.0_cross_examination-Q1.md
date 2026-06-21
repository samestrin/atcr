---
id: mem-2026-06-21-288a78
question: "Should debate survivors use a new DEBATED confidence tier or stay as VERIFIED with a challenge_survived marker?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [internal/reconcile/gate.go, internal/verify/confidence_v2.go, internal/reconcile/emit.go, internal/report/render.go]
tags: [clarifications, epic-6.0_cross_examination, architecture, confidence-tier, debate]
retrievals: 0
status: active
type: clarifications
---

# Should debate survivors use a new DEBATED confidence tier or

## Decision

Use VERIFIED as the top tier and add `challenge_survived: true` on the debate block (option A). The gate at gate.go:96-113 keys on Verification.Verdict, not the Confidence string — a new tier string would be invisible to the existing gate. confidenceV2() at confidence_v2.go:22-31 is the single write path to Confidence; a challenge_survived boolean follows the additive omitempty-safe pattern of PathWarning/PathSuggestion and requires zero changes to the gate, report renderer, or emit path. Option B would require forking confidenceV2 and updating every site that reads or renders Confidence.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/reconcile/gate.go
- internal/verify/confidence_v2.go
- internal/reconcile/emit.go
- internal/report/render.go

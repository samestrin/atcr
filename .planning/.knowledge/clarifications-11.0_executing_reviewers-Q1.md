---
id: mem-2026-06-26-c0f1f9
question: "Should the repro package write-back (repro.Stamp) be wired into verify/pipeline.go, or deleted as out-of-scope?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/repro/repro.go, internal/verify/invoke.go, internal/verify/pipeline.go, internal/reconcile/emit.go, internal/report/render.go]
tags: [clarifications, epic-11.0_executing_reviewers, architecture, repro, evidence_exec, pipeline, SC-3, SC-4]
retrievals: 0
status: active
type: clarifications
---

# Should the repro package write-back (repro.Stamp) be wired i

## Decision

Wire in — SC-3 and SC-4 are explicitly in-scope. invokeSkeptic (invoke.go:50) must be extended to return *reconcile.EvidenceExec; verifyFinding (pipeline.go:439) collects it per-skeptic; the runVerify post-loop (pipeline.go:261-269) calls repro.Stamp(findings[i], verdict, ev) when ev is non-nil. The repro package, emit.go schema, and render.go consumer are all complete — the only missing link is the EvidenceExec threading from invokeSkeptic back to the post-loop. Deleting would break the 'Reproduced' badge path (render.go:186) and schema field (emit.go:130) already shipped.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/repro/repro.go
- internal/verify/invoke.go
- internal/verify/pipeline.go
- internal/reconcile/emit.go
- internal/report/render.go

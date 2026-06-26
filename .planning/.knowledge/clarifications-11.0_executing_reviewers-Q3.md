---
id: mem-2026-06-26-418ac8
question: "Is the two-run repro.Reproduce determinism pass still required, and which findings qualify as eligible?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [internal/verify/pipeline.go, internal/repro/repro.go]
tags: [clarifications, epic-11.0_executing_reviewers, implementation, repro, determinism, two-run, pipeline, T3, SC-3]
retrievals: 0
status: active
type: clarifications
---

# Is the two-run repro.Reproduce determinism pass still requir

## Decision

Yes — still required and still missing. The current wiring (execEvidenceRecorder + repro.Stamp at pipeline.go:278) captures evidence from a single exec-skeptic call but never calls repro.Reproduce, so T3's determinism check is not performed. Eligible findings are those that cleared meetsSeverityFloor (pipeline.go:170) AND had an exec skeptic propose a repro command — no additional high-severity filter beyond the configured severity floor. Call repro.Reproduce(ctx, backend, cmd) after the exec skeptic runs, apply repro.Verdict() to set confirmed/unverifiable, then call repro.Stamp. repro.Verdict (repro.go:34) handles all edge cases correctly; only the pipeline call site is absent.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
- internal/repro/repro.go

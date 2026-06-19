---
id: mem-2026-06-19-576402
question: "Where should panic-path agent metrics be instrumented in the fanout engine?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/fanout/engine.go]
tags: []
retrievals: 0
status: active
type: project
---

# Where should panic-path agent metrics be instrumented in the

## Decision

Instrument the panic path at the engine's existing recover sites, NOT via a deferred outcome-classifier rewrite of ExecuteReview. internal/fanout/review.go ExecuteReview already classifies the review outcome at each explicit return via recordReviewOutcome(interrupted, failed), with `interrupted` derived from ctx.Err() (review.go:373) — there is no panic to recover from there, so consolidating its four explicit returns (review.go:392,408,417,420) into a named-return/recover recorder is a gratuitous control-flow change. internal/fanout/engine.go already has recover wrappers building resultFromPanic(...) in the parallel lane (engine.go:291-295) and serial lane (engine.go:323-327); resultFromPanic is defined at engine.go:247-256. The real defect: those recover sites live in Engine.Run OUTSIDE invokeAgent, so a panicking agent's Result bypasses the per-agent metrics at engine.go:430-434 (atcr_agents_total Inc, atcr_agent_duration_seconds Observe, recordAgentOutcome). Minimal correct fix: inside the two recover blocks, route the resultFromPanic Result through recordAgentOutcome and Observe the agent duration using the wrapper's `start`, and mirror/hoist the atcr_agents_total Inc so panicking agents are counted. Do not touch ExecuteReview. (88% confidence — verify whether a RED test in internal/fanout/*_test.go pins the expected panic-path counter behavior before finalizing.)</answer>
<parameter name="tags">clarifications, epic-4.4_metrics, implementation, architecture, metrics, fanout, panic-recovery

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/fanout/engine.go

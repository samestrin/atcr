---
id: mem-2026-06-19-730a13
question: "Should the recordAgentOutcome deadline-case use max(1, r.Turns) instead of recording 0 API calls (internal/fanout/metrics.go)?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/fanout/metrics.go, internal/fanout/engine_metrics_test.go, internal/fanout/engine.go]
tags: []
retrievals: 0
status: active
type: clarifications
---

# Should the recordAgentOutcome deadline-case use max(1, r.Tur

## Decision

No. In recordAgentOutcome (internal/fanout/metrics.go:37-53) the `apiCalls < 1` branch is only reached when Result.Turns==0. Forcing max(1,Turns) there would count calls that never reached the wire: the circuit-open fail-fast case (Epic 4.5 AC2 mandates NO HTTP request is made) and the cancel-before-send case. The current behavior deliberately records 0 for context.DeadlineExceeded/Canceled and CircuitOpenError, documented at metrics.go:39-43 and locked by two tests (engine_metrics_test.go:38 and :114). The residual undercount (=1 call when a single-shot agent times out mid-flight) is bounded, accepted metric imprecision, not a correctness bug. The only correct fix is threading a real per-call attempt counter out of internal/llmclient onto fanout.Result — the SAME surfacing work the API-call latency histogram was deferred for (metrics.go:22-24). Both belong together in a dedicated telemetry epic, not a one-line patch. Disposition: defer.</answer>
<parameter name="tags">clarifications, epic-4.5_circuit_breaker, architecture, metrics, observability, technical-debt

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/metrics.go
- internal/fanout/engine_metrics_test.go
- internal/fanout/engine.go

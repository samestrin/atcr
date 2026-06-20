# Code Review Stream - 4.5_circuit_breaker (Epic)

**Started:** June 19, 2026 03:48:58PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — After 3 consecutive API failures for a provider, the circuit opens
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/circuitbreaker/breaker.go:158-167` (RecordFailure increments failureCount, opens at threshold), `internal/circuitbreaker/breaker.go:62-65` (DefaultThreshold = 3), test `internal/circuitbreaker/breaker_test.go:61` (TestOpensAfterThresholdFailures), `internal/llmclient/client_breaker_test.go:289` (TestBreaker_RetriesThenTripsPerInvocation — per-invocation granularity)
- **Notes:** Threshold is consecutive (success resets the run, verified by TestClosedSuccessResetsFailureRun). Breaker records one outcome per agent invocation, not per HTTP attempt.

### Criterion: AC2 — When open, API calls fail immediately with CircuitOpenError (no HTTP request)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client.go:308-311` (send checks breaker.Allow() before dispatch, returns &CircuitOpenError), `internal/llmclient/client.go:341-348` (CircuitOpenError type), test `internal/llmclient/client_breaker_test.go:48` (TestBreaker_OpensAfterThree500s asserts 4th call makes no HTTP request, hits stays 3)
- **Notes:** Fail-fast occurs before any http.NewRequestWithContext call.

### Criterion: AC3 — After 60 seconds, the circuit transitions to half-open
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/circuitbreaker/breaker.go:184-188` (refresh: open→half-open when now-openedAt >= cooldown), `internal/circuitbreaker/breaker.go:64` (DefaultCooldown = 60s), tests `breaker_test.go:99` (TestTransitionsToHalfOpenAfterCooldown), `breaker_test.go:111` (TestHalfOpenAdmitsSingleProbe)
- **Notes:** Cooldown applied lazily on Allow()/State() via injectable clock; no timer goroutine.

### Criterion: AC4 — In half-open, 1 successful call closes the circuit
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/circuitbreaker/breaker.go:143-149` (RecordSuccess in HalfOpen → transition(StateClosed)), test `breaker_test.go:122` (TestHalfOpenSuccessCloses)
- **Notes:** Also resets failureCount and clears probeInFlight.

### Criterion: AC5 — In half-open, 1 failure reopens the circuit (resets cooldown)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/circuitbreaker/breaker.go:168-171` (RecordFailure in HalfOpen → open() which re-anchors openedAt), test `breaker_test.go:139` (TestHalfOpenFailureReopens asserts restarted cooldown)
- **Notes:** open() sets openedAt = now(), restarting the 60s window.

### Criterion: AC6 — Fanout engine treats CircuitOpenError as permanent failure and triggers fallback
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine.go:441-470` (invokeSlot descends fallback chain on any non-StatusOK), `internal/fanout/engine.go:546-549` (classifyStatus: CircuitOpenError is not deadline/cancel → StatusFailed, not StatusTimeout), tests `engine_breaker_test.go:38` (TestInvokeSlot_CircuitOpenTriggersFallback), `engine_breaker_test.go:57` (chain fails as StatusFailed)
- **Notes:** Context bridge confirmed: `engine.go:508-512` sets ctx = circuitbreaker.NewContext(ctx, a.Provider) in invokeAgent (single chokepoint for single-shot/tool-loop/verify). Agent.Provider wired in review.go:589 (buildAgent) + review.go:655 (buildFallbackAgent); verify path in verify/invoke.go:108 (buildSkepticAgent). Circuit-open also excluded from atcr_api_calls_total (engine_breaker_test.go:73).

### Criterion: AC7 — atcr_circuit_breaker_state metric reflects current state per provider
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/metrics/names.go:33` (NameCircuitBreakerState), `internal/metrics/names.go:39` (LabelProvider), `internal/circuitbreaker/breaker.go:210-218` (transition/setMetric writes 0/1/2 to provider-labelled gauge on every transition), gauge primitive `internal/metrics/metrics.go:151-167` (Set/Value), Prometheus `# TYPE … gauge` exposition `internal/metrics/prometheus.go:102-116`, gauge assertions in breaker_test.go (gaugeValue 0/1/2)
- **Notes:** Metric shape = Option A (numeric value 0=closed/1=open/2=half-open), per recorded clarification. Gauge seeded to closed (0) at New().

### Criterion: AC8 — go test ./internal/circuitbreaker/... passes with 100% coverage of state transitions
- **Verdict:** VERIFIED ✅
- **Evidence:** `go test -cover ./internal/circuitbreaker/...` → 98.5% overall; `go tool cover -func` confirms breaker.go is 100% (all transition funcs: Allow, RecordSuccess, RecordFailure, ReleaseProbe, refresh, open, transition, State). Only gap is registry.go Get 91.7% (concurrent double-checked-lock re-check window — not a state transition).
- **Notes:** AC scoped to "state transitions" — fully covered. Overall 98.5% gap is a registry concurrency branch, not breaker logic.

### Criterion: AC9 — Integration test: mock provider returning 500, verify circuit opens after 3 failures
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client_breaker_test.go:48` (TestBreaker_OpensAfterThree500s — httptest server returns 500, asserts StateOpen after 3 calls and CircuitOpenError on 4th)
- **Notes:** Uses real httptest.Server + real Client (integration-level, not a pure unit mock).

### Criterion: AC10 — Error classification: 5xx/timeout/transport trip; all 4xx (incl 429/401) stay closed; cancellation records nothing
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/llmclient/client.go:351-377` (isBreakerFailure: 5xx via HTTPStatusError.Status>=500, transport default true, context.Canceled false, 4xx false), `internal/llmclient/client.go:316-340` (send 3-way switch: success/breaker-failure/cancel-release/4xx-as-reachable), tests: TestBreaker_429StaysClosed (client_breaker_test.go:79), TestBreaker_401StaysClosed (:99), TestBreaker_TransportFailureTrips (:119), TestBreaker_CancellationDoesNotTrip (:191), TestIsBreakerFailureClassification (:217)
- **Notes:** Extended AC10 (transport errors trip) fully implemented and tested per the 2026-06-19 rubber-duck clarification. AC10 wording in the epic was correctly updated to match.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (epic — no sprint-design.md risk profile)
**Files Reviewed:** 19 (.go changed files, 4 hostile-review batches)
**Issues Found:** 18 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 18

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 15

**Reviewer note:** None of the findings invalidate any acceptance criterion — all 10 ACs pass and all quality gates are green. The 3 MEDIUM items are refinements: (1) a panic in `dispatch` could leak the half-open probe slot (no `defer` guard); (2) a client-side `DeadlineExceeded` is counted as a breaker failure and can reopen a half-open circuit even when the provider is healthy-but-slow; (3) `recordAgentOutcome` zeroes `atcr_api_calls_total` on a deadline that may have followed real round-trips. The core state machine, registry double-checked locking, atomic gauge, label escaping, and provider threading were all hostile-reviewed and found correct.

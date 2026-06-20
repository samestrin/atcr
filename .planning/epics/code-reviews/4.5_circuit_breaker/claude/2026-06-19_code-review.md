# Code Review Report: 4.5_circuit_breaker

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 10 / 10
- **Approval Status:** Approved
- **Review Date:** June 19, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)
- **Merge Commit:** d522555 (PR #48)

## 2. Acceptance Criteria Verified

| AC | Result | Primary Evidence |
|----|--------|------------------|
| AC1 — opens after 3 consecutive failures | ✅ VERIFIED | `internal/circuitbreaker/breaker.go:158-167`, `:62-65`; test `breaker_test.go:61` |
| AC2 — open fails fast with CircuitOpenError, no HTTP | ✅ VERIFIED | `internal/llmclient/client.go:308-311`, `:341-348`; test `client_breaker_test.go:48` |
| AC3 — half-open after 60s | ✅ VERIFIED | `internal/circuitbreaker/breaker.go:184-188`, `:64`; tests `breaker_test.go:99,111` |
| AC4 — half-open success closes | ✅ VERIFIED | `internal/circuitbreaker/breaker.go:143-149`; test `breaker_test.go:122` |
| AC5 — half-open failure reopens + resets cooldown | ✅ VERIFIED | `internal/circuitbreaker/breaker.go:168-171`; test `breaker_test.go:139` |
| AC6 — fanout treats CircuitOpenError as permanent → fallback | ✅ VERIFIED | `internal/fanout/engine.go:441-470,546-549,508-512`; tests `engine_breaker_test.go:38,57` |
| AC7 — atcr_circuit_breaker_state metric per provider | ✅ VERIFIED | `internal/metrics/names.go:33,39`; `breaker.go:210-218`; `metrics.go:151-167`; `prometheus.go:102-116` |
| AC8 — 100% coverage of state transitions | ✅ VERIFIED | breaker.go 100% (all transition funcs); package 98.5% (gap is registry.Get concurrency branch) |
| AC9 — integration test 500 → opens after 3 | ✅ VERIFIED | `internal/llmclient/client_breaker_test.go:48` (real httptest.Server) |
| AC10 — error classification (5xx/timeout/transport trip; 4xx incl 429/401 stay closed; cancel records nothing) | ✅ VERIFIED | `internal/llmclient/client.go:351-377,316-340`; tests `client_breaker_test.go:79,99,119,191,217` |

## 3. Evidence Map (key integration points)
- **Provider threading (context bridge):** `internal/fanout/engine.go:508-512` sets `ctx = circuitbreaker.NewContext(ctx, a.Provider)` in `invokeAgent` — one chokepoint covering single-shot, tool-loop, and verify paths. `llmclient.send` reads it (`client.go:303-307`).
- **Agent.Provider wiring:** `internal/fanout/review.go:589` (buildAgent), `:655` (buildFallbackAgent — threads the fallback's OWN provider, so a healthy fallback isn't blocked by the primary's open circuit); `internal/verify/invoke.go:108` (buildSkepticAgent).
- **State machine:** three-state (closed/open/half-open) with injectable clock; single-probe half-open gate; gauge updated on every transition.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 10 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria implemented with direct code + test evidence. The state machine, registry double-checked locking, atomic gauge, Prometheus label escaping, and provider threading were hostile-reviewed and found correct. Findings are non-blocking tech debt.

## 6. Coverage Analysis
- **Coverage:** 89.4% (total)
- **Baseline:** 80%
- **Delta:** ↑9.4%
- **Status:** PASSING
- Epic packages: circuitbreaker 98.5%, llmclient 94.5%, metrics 100%, fanout 85.8%, verify 94.6%.

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Tests | PASSING (0 failures) | go test ./... |
| Lint | PASSING (0 issues) | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | gofmt -l (clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 19 (.go changed files, 4 hostile-review batches)
- **Issues Found:** 18 (Critical: 0, High: 0, Medium: 3, Low: 15)
- **Mode:** Discovery (epic — no sprint-design risk profile)

### Medium
1. **`internal/llmclient/client.go:312`** — `send()` resolves the half-open probe in a plain switch with no `defer` guard; a panic in `dispatch` would leak the probe slot and wedge the circuit half-open forever. (Independent reviewer rated HIGH; downgraded — a dispatch panic is unexpected and the engine recovers slot panics.)
2. **`internal/llmclient/client.go:357`** — `isBreakerFailure` counts `context.DeadlineExceeded` as a breaker failure, so a client-side/per-agent deadline can trip or reopen a circuit for a healthy-but-slow provider. AC10 intends provider timeouts to trip but does not distinguish caller-initiated deadlines.
3. **`internal/fanout/metrics.go:37`** — `recordAgentOutcome` zeroes `atcr_api_calls_total` on any deadline/cancel, undercounting real round-trips that completed before a per-agent timeout fired.

### Low (15)
Defensive/maintainability items: `New()` threshold/cooldown validation; `setMetric` holding `b.mu` across the metrics lock (cache the gauge pointer); `ReleaseProbe` code/comment mismatch; unbounded probe slot-hold; no structured log on state transitions (Epic 4.0 relationship); `readErrorSnippet` 8KB drain cap (pre-existing); 3xx counted as health; limited key-prefix redaction (pre-existing); `Registry.Get("")` unguarded; AC6 emergent — add contract comment; empty-Provider silent breaker bypass; `gauge.Set` missing NaN/Inf guard (asymmetry with `histogram.Observe`); gauge exposition NaN render; stale `Reset` doc comment; Prometheus family-grouping copy-paste. Full detail in `td-stream.txt`.

## 9. Follow-ups
- Findings are non-blocking and written to `code-review/4.5_circuit_breaker/claude/td-stream.txt`.
- Next: `/reconcile-code-review` to merge into the technical-debt README with reviewer/confidence attribution.

---
*Generated by /execute-code-review on June 19, 2026 03:48:58PM*

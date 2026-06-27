---
id: TD-0023
order: 23
section: '[2026-06-19] From Sprint: 4.5_circuit_breaker'
date: "2026-06-19"
group: "2"
status: deferred
severity: MEDIUM
file: internal/fanout/metrics.go:37
category: performance
est_minutes: "60"
source: code-review
reviewers: claude
confidence: MEDIUM
has_review_cols: true
---

## Problem

recordAgentOutcome zeroes apiCalls for any result whose error unwraps to context.DeadlineExceeded/Canceled, assuming no request was made. But a per-agent timeout routinely fires AFTER real HTTP round-trips (and mid tool-loop after several Chat turns already hit the wire), so atcr_api_calls_total undercounts real provider traffic exactly when a provider is degraded. The no-request assumption is only provably safe for CircuitOpenError and pre-first-send cancellation. (Deferred: Epic Plan 4.11)

## Fix

Use max(1, r.Turns) for the deadline case instead of a flat 0, or thread a real calls-attempted counter out of the client rather than inferring from the terminal error class. Keep the apiCalls=0 path only for CircuitOpenError.

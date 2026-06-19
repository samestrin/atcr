---
id: mem-2026-06-19-a2be50
question: "How should the atcr_circuit_breaker_state gauge be encoded, and what does the metrics package support?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/metrics/prometheus.go, internal/metrics/metrics.go, internal/metrics/names.go]
tags: []
retrievals: 0
status: active
type: clarifications epic 4.5_circuit_breaker
---

# How should the atcr_circuit_breaker_state gauge be encoded, 

## Decision

Use numeric-value single-label encoding: `atcr_circuit_breaker_state{provider="..."} 0|1|2` (0=closed,1=open,2=half-open). The metrics package's `Key(name,label,value)` builder is single-label only (internal/metrics/prometheus.go:22-24); a per-state two-label encoding would need a new key builder. Decisive prerequisite for EITHER encoding: the metrics package has NO gauge type — only a monotonic counter (Inc/Add, cannot decrement; internal/metrics/metrics.go:35-48) and a histogram (metrics.go:57-72). Circuit state must go up and down, so a new settable gauge primitive must be added regardless. Existing labeled-metric precedent is enum-as-single-label-VALUE on counters (atcr_api_errors_total{status}, atcr_findings_by_severity{severity}; internal/metrics/names.go:24-34), not 2-label gauges. Given the shared gauge cost, the minimal-label numeric encoding adds least surface area; defer a per-state series only if a concrete dashboard/alert needs `state` as a queryable PromQL label.</answer>
<parameter name="tags">clarifications, epic-4.5_circuit_breaker, metrics, observability, prometheus, gauge

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/metrics/prometheus.go
- internal/metrics/metrics.go
- internal/metrics/names.go

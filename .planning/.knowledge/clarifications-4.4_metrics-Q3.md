---
id: mem-2026-06-19-3b8edd
question: "How should labeled counters be modeled in the internal/metrics package (e.g., atcr_api_errors_total by HTTP status, atcr_findings_by_severity by severity)?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/metrics/metrics.go, internal/metrics/prometheus.go]
tags: [clarifications, epic-4.4_metrics, architecture, metrics, prometheus]
retrievals: 0
status: active
type: epic-4.4_metrics clarifications 2026-06-19
---

# How should labeled counters be modeled in the internal/metri

## Decision

Model labeled counters as distinct Counter instances keyed by the full Prometheus-style string in the registry's counters map (e.g., atcr_api_errors_total{status="429"}). This gives a zero-surface-area label API — no generic label/tag fields on Counter, no variadic constructors — consistent with the epic's decision to use a custom implementation without prometheus/client_golang. The Prometheus text renderer extracts the base metric family name by splitting on '{' (via a metricFamily() helper) and emits one # TYPE line per family, then lists each keyed variant on its own line. AC7 is satisfied: callers write metrics.Counter("atcr_api_errors_total{status=\"429\"}").Inc() which is unambiguous and directly testable with Value().

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/metrics/metrics.go
- internal/metrics/prometheus.go

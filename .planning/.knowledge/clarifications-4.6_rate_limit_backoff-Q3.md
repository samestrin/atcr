---
id: mem-2026-06-19-30bd7a
question: "Which retry knobs should Epic 4.6 expose in config — just max_retries + initial_backoff_ms, or also backoff_factor and max_backoff_ms?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go]
tags: [clarifications, epic-4.6_rate_limit_backoff, scope, config, retry]
retrievals: 0
status: active
type: clarifications
---

# Which retry knobs should Epic 4.6 expose in config — just 

## Decision

Only max_retries and initial_backoff_ms. backoff_factor (defaultBackoffFactor=1.5 at client.go:30) and the maxBackoff cap (client.go:627) stay as unexposed constants. Exposing them adds schema surface and validation complexity the AC does not require. initial_backoff_ms is necessary because the AC names "base delays" explicitly and users with aggressive providers need to tune the fallback schedule.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go

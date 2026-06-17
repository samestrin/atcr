---
id: mem-2026-06-17-ad7897
question: "Should log.New enforce single-occurrence correlation keys (review_id/agent_name) with a handler-level dedup guard, or is the documented single-wrap contract sufficient?"
created: 2026-06-17
last_retrieved: ""
sprints: [4.0_structured_logging]
files: [internal/log/correlation.go, internal/log/log.go, cmd/atcr/review.go, internal/fanout/engine.go, internal/mcp/handlers.go]
tags: [clarifications, sprint-4.0_structured_logging, architecture, logging, slog, correlation]
retrievals: 0
status: active
type: clarifications
---

# Should log.New enforce single-occurrence correlation keys (r

## Decision

Document the single-wrap contract; do NOT add a handler-level dedup guard. Correlation keys are appended (not replaced) by slog, so each of WithReviewID/WithAgent must be called once per key on distinct loggers. All real call sites already do this exactly once: review_id at cmd/atcr/review.go:262 and internal/mcp/handlers.go:87, agent_name at internal/fanout/engine.go:420 (the engine receives the review_id-correlated logger and only adds agent_name — never re-adds review_id). log.New uses stock slog Text/JSON handlers with only a Level option and no ReplaceAttr (internal/log/log.go:87-97); a ReplaceAttr collapse would impose a per-attribute callback on every log line through the shared pipeline to defend a programming error no call site makes. Requirements mandate correlation-attribute attachment only (original-requirements.md:67,134), not idempotent wrapping. Mitigation is the call-once doc contract (internal/log/correlation.go:18-21) plus TestCorrelation_DoubleWrapAppends; residual dedup-enforcement is deferred to TD-006.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/log/correlation.go
- internal/log/log.go
- cmd/atcr/review.go
- internal/fanout/engine.go
- internal/mcp/handlers.go

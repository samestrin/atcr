---
id: mem-2026-07-11-7bf744
question: "TD-002: degenerate model window (eff==0) should resolve independently of TD-004, not with a byte floor"
created: 2026-07-11
last_retrieved: ""
sprints: [19.10_reviewer_payload_sizing]
files: [internal/fanout/review.go, internal/payload/sizing.go, internal/payload/contextwindow.go]
tags: [clarifications, sprint-19.10_reviewer_payload_sizing, implementation, td-002, atcr-payload-sizing]
retrievals: 0
status: active
type: clarifications
---

# TD-002: degenerate model window (eff==0) should resolve inde

## Decision

When EffectiveByteBudget returns 0 (window <= output+promptOverhead), do not gate the fix on TD-004's on_overflow wiring and do not use a positive byte floor — a window that small has zero room for any input regardless of floor value, so a floor cannot fix an unfittable window. Instead reuse the bulk-shed branch's existing AllDropped precedent (internal/fanout/review.go): set bulkDegradation = "overflow" and emit the existing oversized-payload warning, rather than leaving the agent's degradation_action silently unmarked while it keeps the full un-sheded global payload. This is a diagnosability fix, not a payload-size fix — the codebase's one existing floor precedent (minChunkLines=64 in internal/payload/sizing.go) is a line-count clamp for the chunk path, not reusable here. Confirmed unreachable today (smallest roster window and unknown-model default are both 32768, well above the 12288-token threshold).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/payload/sizing.go
- internal/payload/contextwindow.go

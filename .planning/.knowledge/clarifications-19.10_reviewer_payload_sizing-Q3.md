---
id: mem-2026-07-11-345076
question: "Uncounted SCOPE CONSTRAINT bytes reintroduce overflow on --sprint-plan path — fix bulk AND chunked paths together"
created: 2026-07-11
last_retrieved: ""
sprints: [19.10_reviewer_payload_sizing]
files: [internal/fanout/review.go, internal/payload/sprintplan.go, internal/payload/sizing.go]
tags: [clarifications, sprint-19.10_reviewer_payload_sizing, implementation, atcr-payload-sizing, sprint-plan-scoping]
retrievals: 0
status: active
type: clarifications
---

# Uncounted SCOPE CONSTRAINT bytes reintroduce overflow on --s

## Decision

The sprint-plan SCOPE CONSTRAINT block (up to max_sprint_plan_bytes, default 64KiB) is prepended to every agent's payload in renderAgent AFTER the per-agent byte budget/chunk-line decision is already made, so its byte cost is uncounted against appliedBudget (bulk path) and ChunkMaxLines (chunked path) alike — both derive purely from EffectiveByteBudget(model, outputTokens) with no scopeConstraint parameter. Fix: combine (A) subtracting len(scopeConstraint) from the budget available before shedding/chunking, with (B) capping the per-agent plan body via min(max_sprint_plan_bytes, EffectiveByteBudget/8) inside the add closure — B subsumes A's goal more completely since A alone doesn't shrink an oversized plan. The chunked path needs identical treatment to the bulk path since ChunkMaxLines shares the exact same EffectiveByteBudget gap — this is confirmed by code inspection, not just analogy. This was a known, deliberately-simplified omission from the original F2/F3 sizing work (documented in internal/payload/sizing.go), not an accidental bug.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/payload/sprintplan.go
- internal/payload/sizing.go

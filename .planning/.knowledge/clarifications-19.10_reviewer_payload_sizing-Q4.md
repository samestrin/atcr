---
id: mem-2026-07-11-3ab763
question: "TD-005: aggregate timeout wave-count must use ALL parallel-lane slots, not just chunked slots"
created: 2026-07-11
last_retrieved: ""
sprints: [19.10_reviewer_payload_sizing]
files: [internal/fanout/timeout.go, internal/fanout/review.go, internal/fanout/engine.go]
tags: [clarifications, sprint-19.10_reviewer_payload_sizing, implementation, td-005, atcr-payload-sizing, timeout-scaling]
retrievals: 0
status: active
type: clarifications
---

# TD-005: aggregate timeout wave-count must use ALL parallel-l

## Decision

When deriving an aggregate deadline from wave count (ceil(slots / max_parallel)) for a shared worker pool, the slot count must include ALL slots contending for that pool — not just the subset relevant to the specific feature being scaled (here: chunked slots for timeout scaling). internal/fanout/engine.go's semaphore is shared by every non-serial (parallel-lane) slot regardless of whether it's chunked, so counting only chunked slots undercounts true contention when non-chunked personas also occupy semaphore tokens. Serial-lane slots are correctly excluded — they run on a dedicated goroutine outside the semaphore, concurrent with the parallel lane. Threading a resolved config value (max_parallel) into a helper is safe and low-risk when the value is already resolved and in scope at the same call site as the helper invocation (confirm via the calling function before assuming a new lookup is needed).

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/timeout.go
- internal/fanout/review.go
- internal/fanout/engine.go

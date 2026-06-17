---
id: mem-2026-06-17-c0dfad
question: "Should internal/errors IsRetryable adopt a \"Permanent poisons the chain\" guard (constructors refuse to escalate an inner non-retryable error to Transient), or is outermost-classification-wins intentional?"
created: 2026-06-17
last_retrieved: ""
sprints: [4.0_structured_logging]
files: [internal/errors/errors.go, internal/errors/errors_test.go]
tags: [clarifications, sprint-4.0_structured_logging, architecture, error-handling, errors-package, retryability]
retrievals: 0
status: active
type: clarifications
---

# Should internal/errors IsRetryable adopt a "Permanent poison

## Decision

Outermost-classification-wins is the intentional, documented contract — do NOT add a "Permanent poisons the chain" guard now. IsRetryable resolves the outermost *ClassifiedError via errors.As "so the most recent, most-informed classifier decides" (internal/errors/errors.go:34-39, 95-100), and the behavior is pinned by passing test TestIsRetryable_DoubleWrappedOutermostWins (internal/errors/errors_test.go:196-201) asserting NewTransient(NewPermanent(err)) IS retryable. No code re-wraps a Permanent as Transient today (constructors at errors.go:63-93 set Retryable unconditionally; no generic retry wrapper exists per Epic 4.0 Open Question 4 = incremental/opt-in classification). The guard + regression test should be added only if/when a double-classification path (e.g. a generic retry wrapper, Epic 4.5 circuit-breaker) is introduced; keep the security note as the trigger condition rather than reversing the design.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/errors/errors.go
- internal/errors/errors_test.go

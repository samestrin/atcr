---
id: mem-2026-06-17-f687d8
question: "internal/errors/errors.go:75 \"Error message not wrapped with context\" — what context should be added and does it conflict with the delegate-to-underlying contract?"
created: 2026-06-17
last_retrieved: ""
sprints: [4.0_structured_logging]
files: [internal/errors/errors.go]
tags: [clarifications, sprint-4.0_structured_logging, implementation, error-handling, errors-package, error-wrapping-contract]
retrievals: 0
status: active
type: clarifications
---

# internal/errors/errors.go:75 "Error message not wrapped with

## Decision

False positive — no change. errors.go:75 is the `return nil` inside NewPermanent's nil-guard (no error message exists there to wrap). The errors package deliberately delegates Error() to the underlying error's message and carries classification as the structured Classification field, not a string prefix (internal/errors/errors.go:46-55); constructors are intentionally thin wrappers attaching only Classification + Retryable. Adding a context prefix (e.g. fmt.Errorf("permanent: %w", err)) would violate the documented delegate-to-underlying contract, change Error() output that tests assert on, and add no value over the structured field. Resolve the TD row as by-design / won't-fix.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/errors/errors.go

---
id: mem-2026-06-19-6a1df3
question: "After Epic 4.6 exhausts retries on a 429, should that terminal failure count toward the Epic 4.5 circuit breaker?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go, internal/circuitbreaker/breaker.go]
tags: [clarifications, epic-4.6_rate_limit_backoff, architecture, circuit-breaker, 429, boundary]
retrievals: 0
status: active
type: clarifications
---

# After Epic 4.6 exhausts retries on a 429, should that termin

## Decision

No — keep unchanged. Existing code already implements the correct behavior: dispatch() wraps an exhausted 429 in atcrerrors.NewTransient("exhausted retries: %w", &HTTPStatusError{Status:429}). In send(), isBreakerFailure() does errors.As through ClassifiedError.Unwrap(), reaches HTTPStatusError{Status:429}, and returns false (429 >= 500 is false). The default branch at client.go:354 fires and calls breaker.RecordSuccess(). The code comment explicitly names "4xx incl. 429/401" as non-tripping. No code change needed for this boundary.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go
- internal/circuitbreaker/breaker.go

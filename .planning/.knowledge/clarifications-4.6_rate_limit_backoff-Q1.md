---
id: mem-2026-06-19-afe9e0
question: "Should Epic 4.6 build a new RetryMiddleware type or reuse the existing dispatch() retry engine in client.go?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go]
tags: [clarifications, epic-4.6_rate_limit_backoff, architecture, llmclient, retry]
retrievals: 0
status: active
type: clarifications
---

# Should Epic 4.6 build a new RetryMiddleware type or reuse th

## Decision

Reuse the existing engine — do NOT build a new RetryMiddleware. dispatch() at internal/llmclient/client.go:408 is the complete retry engine: exponential backoff with jitter, 429 handling via retryableStatus (line 45), and Retry-After parsing via parseRetryAfter (line 608). All production call sites use llmclient.New() with no options. Epic 4.6 = thread resolved config values into WithRetry() at construction time. A second mechanism would duplicate working, tested code and contradict the epic's own boundary note.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go

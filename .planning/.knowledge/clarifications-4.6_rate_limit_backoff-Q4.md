---
id: mem-2026-06-19-2fd25e
question: "For Epic 4.6 max_retries default of 5 — change the defaultMaxRetries constant in client.go or set 5 as the embedded-tier default in ResolveSettings?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go, internal/registry/precedence.go, cmd/atcr/doctor.go]
tags: [clarifications, epic-4.6_rate_limit_backoff, implementation, defaults, llmclient, registry]
retrievals: 0
status: active
type: clarifications
---

# For Epic 4.6 max_retries default of 5 — change the default

## Decision

Change the embedded-tier default in ResolveSettings to 5, NOT the defaultMaxRetries=2 constant in client.go:27. The split matters: defaultMaxRetries=2 remains the naked-New() fallback for the doctor self-test (cmd/atcr/doctor.go:90) and bare-client tests. The production review path reads Settings.MaxRetries and passes it via WithRetry() at construction time — so production gets 5 by default without inflating the doctor's retry budget or test clients that construct a bare llmclient.New().

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go
- internal/registry/precedence.go
- cmd/atcr/doctor.go

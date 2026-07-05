---
id: mem-2026-07-05-3ea877
question: "HTTP client timeout convention: match operation shape, not a global default"
created: 2026-07-05
last_retrieved: ""
sprints: []
files: [internal/llmclient/client.go, internal/registry/config.go]
tags: [clarifications, epic-19.2_shared_registry_remote, implementation, http-client, conventions]
retrievals: 0
status: active
type: clarifications
---

# HTTP client timeout convention: match operation shape, not a

## Decision

internal/llmclient uses a 120s HTTP client timeout (defaultHTTPTimeout, client.go:33), but that budgets a full variable-length LLM chat-completion round trip. It is not a project-wide default to reuse for other HTTP fetches — a new, unrelated fetch (e.g. a one-shot static config file GET) should get its own, shorter timeout constant sized to its own operation shape (e.g. ~10s), following the same "always use a bounded http.Client, never a bare http.Get" pattern but not the same numeric value. Similarly, URL scheme validation elsewhere in the registry package (base_url) already accepts both http:// and https:// with no preference — match that existing permissiveness (with a warning on non-https) rather than inventing a stricter https-only rule for a new URL-accepting field, unless the security context specifically differs.

Justification:
- internal/llmclient/client.go:33 sets defaultHTTPTimeout = 120 * time.Second, applied at client.go:106-107 for LLM provider calls.
- internal/registry/config.go (validateProvider, ~line 652-658) accepts both http and https schemes for base_url with no preference between them.
- No existing http.Client construction existed in internal/registry prior to this — confirm via grep before assuming a convention exists to match.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/llmclient/client.go
- internal/registry/config.go

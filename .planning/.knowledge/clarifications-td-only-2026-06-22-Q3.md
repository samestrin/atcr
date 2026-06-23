---
id: mem-2026-06-22-5ddd57
question: "How should postInlineComments distinguish a 422 (benign off-diff or stale SHA) from systemic auth errors (401/403) when CreatePRReview fails?"
created: 2026-06-22
last_retrieved: ""
sprints: []
files: [internal/ghaction/client.go, cmd/atcr/github.go]
tags: [td-clarification, td-only, ghaction, APIError, 422, error-handling, postInlineComments, github-action]
retrievals: 0
status: active
type: clarifications skill 2026-06-22
---

# How should postInlineComments distinguish a 422 (benign off-

## Decision

Introduce a typed error in ghaction/client.go: `type APIError struct { StatusCode int; Message string }` with `Error() string`. Have postDo return `&APIError{StatusCode: resp.StatusCode, Message: githubMessage(respBody)}` for non-2xx responses that are not retried (current retry set is >=500 or ==429 only; 422 falls through). In postInlineComments (github.go:186) use `errors.As(err, &apiErr)` to inspect the status: if apiErr.StatusCode == 422 → log warning to stderr, return (0, deduped, nil); if 401/403 → return codedError{exitFailure}. This is necessary because postDo currently returns a plain error with no status code, so callers cannot distinguish benign 422 from auth failures. The batch CreatePRReview can 422 on stale commit SHA or malformed review body.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/ghaction/client.go
- cmd/atcr/github.go

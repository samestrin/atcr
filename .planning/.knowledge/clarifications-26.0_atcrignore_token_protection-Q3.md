---
id: mem-2026-07-14-322ea7
question: "CLI convention: pair safety-default behavior with an explicit opt-out flag"
created: 2026-07-14
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, internal/fanout/review.go, internal/fanout/resume.go]
tags: [clarifications, epic-26.0_atcrignore_token_protection, architecture, cli-convention]
retrievals: 0
status: active
type: clarifications
---

# CLI convention: pair safety-default behavior with an explici

## Decision

ATCR's cmd/atcr consistently pairs a default-safe behavior with a narrow, well-documented bypass flag rather than making behavior unconditional. Precedent: --no-cache (cmd/atcr/review.go:75) bypasses the diff cache read via ReviewRequest.NoCache threaded through internal/fanout/review.go:63,92,96 and consumed at internal/fanout/resume.go:334; --force bypasses collision protection (review.go:74); --exec requires opt-in for sandboxed reproduction (review.go:69, gated at review.go:211-213); --fresh/--thorough modify default verify behavior. New always-on safety features (e.g. the .gitignore/.atcrignore filter) should follow this same pattern: default-on filtering with a --no-ignore-style bool flag threaded through ReviewRequest, not an unconditional/non-bypassable behavior.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- internal/fanout/review.go
- internal/fanout/resume.go

---
id: mem-2026-06-26-802486
question: "How should resolveExec be ordered relative to gitrange.Resolve and LoadReviewConfig in review.go, and should it accept the already-loaded project config?"
created: 2026-06-26
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, cmd/atcr/verify.go, internal/fanout/review.go]
tags: [clarifications, epic-11.0_executing_reviewers, implementation, review-cmd, resolveExec, config-loading]
retrievals: 0
status: active
type: clarifications
---

# How should resolveExec be ordered relative to gitrange.Resol

## Decision

Move resolveExec to after LoadReviewConfig in review.go RunE, and refactor it to accept the already-loaded cfg.Project (*registry.ProjectConfig) instead of calling registry.LoadProjectConfig internally. gitrange.Resolve and LoadReviewConfig are fast local git/disk reads (not API calls), so placing resolveExec after them still achieves fail-fast before the expensive fanout. This eliminates the double-load of ProjectConfig (once inside resolveExec at verify.go:49, once inside LoadReviewConfig at fanout/review.go:120) and makes review.go consistent with the ordering already established in verify.go:77-82.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- cmd/atcr/verify.go
- internal/fanout/review.go

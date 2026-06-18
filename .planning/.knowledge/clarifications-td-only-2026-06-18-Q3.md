---
id: mem-2026-06-18-f6f668
question: "Where should correlateAndRedact and reportInterrupt shared helpers live, and what are their signatures?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [cmd/atcr/resume.go, cmd/atcr/review.go, cmd/atcr/run_helpers.go]
tags: [td-clarification, td-only, maintainability, refactoring, go, cmd/atcr]
retrievals: 0
status: active
type: td-clarification
---

# Where should correlateAndRedact and reportInterrupt shared h

## Decision

Extract into a new file cmd/atcr/run_helpers.go (package main). Signatures: correlateAndRedact(ctx context.Context, id, repo string) context.Context — encapsulates correlateReviewID + log.WithRedactor setup at resume.go:109-113 / review.go:183-190. reportInterrupt(cmd *cobra.Command, ctx context.Context, result *fanout.ReviewResult, prep *fanout.PreparedReview) error — encapsulates the structured Warn + Fprint + codedError block at resume.go:150-153 / review.go:207-212. This refactor requires editing both review.go and resume.go, so /resolve-td must run without --group to permit cross-file edits.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/resume.go
- cmd/atcr/review.go
- cmd/atcr/run_helpers.go

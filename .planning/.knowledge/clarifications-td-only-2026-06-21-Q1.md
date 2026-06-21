---
id: mem-2026-06-21-1fb1e6
question: "When debate failure leaves verify findings on disk (no rollback), what behavior should the CLI chaining at cmd/atcr/review.go implement: atomic temp-staging, rollback/delete, or accept partial state?"
created: 2026-06-21
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go:306, internal/debate/debate.go:146]
tags: [td-clarification, td-only, architecture, debate, verify, partial-state, error-handling]
retrievals: 0
status: active
type: td-clarification
---

# When debate failure leaves verify findings on disk (no rollb

## Decision

Option (c) — accept the partial state and document it with a comment. Verify findings on disk are valid, structured artifacts (confirmed/refuted/unverifiable verdicts) the user can inspect; debateFailureError already surfaces the failure. Atomic staging (option a) requires significant refactoring of how verify and debate share result.Dir. Rollback (option b) destroys useful debugging state. The debate package's own internal durability concern (applyRulings → writeFindings → writeDebateFile ordering) is tracked separately at internal/debate/debate.go:146. Fix: add a brief comment at cmd/atcr/review.go just before the `if debateFlag` block (~line 306) documenting that a debate failure intentionally leaves verify findings on disk.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go:306
- internal/debate/debate.go:146

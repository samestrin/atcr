---
id: mem-2026-06-19-44e914
question: "Should --resume and --force be mutually exclusive in atcr review, and what exit code and guard placement?"
created: 2026-06-19
last_retrieved: ""
sprints: []
files: [cmd/atcr/review.go, .planning/epics/completed/4.1.1_resume_support.md]
tags: [clarifications, epic-4.7_idempotency, implementation, mutual-exclusion, usageError, resume, force]
retrievals: 0
status: active
type: clarifications
---

# Should --resume and --force be mutually exclusive in atcr re

## Decision

Yes — exit 2 via usageError() is correct and consistent. The existing codebase already uses usageError() for mutually exclusive flags (e.g., --output-dir + --id at cmd/atcr/review.go:61), establishing exit 2 as the pattern. The conflict guard should be added at the --resume branch point (review.go:87-91) before its short-circuit so the error fires regardless of which flag is evaluated first. Message: "--resume and --force are mutually exclusive". Epic 4.1.1 (completed/4.1.1_resume_support.md) says nothing about --force exclusivity since --force did not exist yet — AC1b is a new contract owned entirely by epic 4.7 with no conflicting prior decisions.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/review.go
- .planning/epics/completed/4.1.1_resume_support.md

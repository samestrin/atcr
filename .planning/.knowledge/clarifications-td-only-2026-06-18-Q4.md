---
id: mem-2026-06-18-cdf59a
question: "Should the interrupted-stamp race at fanout/review.go:361 (shutdown fires after all agents succeed but before ctx.Err() check, stamping a complete run as Interrupted) be fixed with a gate on agent StatusTimeout, or is it self-healing?"
created: 2026-06-18
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/fanout/resume.go, internal/fanout/status.go]
tags: [td-clarification, td-only, correctness, fanout, interrupt-handling, resume, self-healing]
retrievals: 0
status: active
type: clarifications td-only 2026-06-18
---

# Should the interrupted-stamp race at fanout/review.go:361 (s

## Decision

Won't-fix for now — it is self-healing. The race window is microscopic (nanoseconds between last agent result write and the ctx.Err() check at review.go:361). ClearInterrupted at fanout/resume.go:220-236 detects AllComplete() on the next --resume invocation and rewrites Interrupted=false, covered by TestResume_AllCompleteClearsStaleInterrupted. The proposed fix (gating interrupted on at least one agent StatusTimeout entry) would touch CLI-shared review.go — explicitly out of scope for epic 4.1.2. AC4 only protects the opposite direction (interrupted→completed). If self-healing is ever deemed insufficient, the minimal fix at review.go:361 belongs in a future backlog sprint.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/fanout/resume.go
- internal/fanout/status.go

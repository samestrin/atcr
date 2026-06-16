---
id: mem-2026-06-16-1d3eaf
question: "Why was the os.Stderr vs cmd.ErrOrStderr() threading deferred to a separate epic?"
created: 2026-06-16
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: []
tags: [sprint-learning, 3.3_per_run_scorecard, architecture, epic-3.4]
retrievals: 0
status: active
type: sprint-learning
---

# Why was the os.Stderr vs cmd.ErrOrStderr() threading deferre

## Decision

Sprint 3.3 discovered that the entire internal/scorecard package writes diagnostics to os.Stderr instead of threading cmd.ErrOrStderr() from cobra. This was deferred to Epic 3.4 (scorecard-diagnostics-writer) as a package-wide cross-cutting refactor rather than fixing only the new Phase 3 code (which would be inconsistent). Lesson: when a cross-cutting concern (diagnostic writer threading) spans an entire package, defer to a dedicated epic rather than patching individual call sites — partial fixes create inconsistency worse than the original problem.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

N/A

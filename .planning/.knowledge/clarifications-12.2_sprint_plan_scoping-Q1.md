---
id: mem-2026-06-27-54bba1
question: "In ATCR, do PrepareReview or PrepareReviewFromDiff contain raw os.Stderr writes that would cause noise when called in a benchmark loop?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go, internal/payload/sprintplan.go, cmd/atcr/benchmark_run.go]
tags: [clarifications, epic-12.2_sprint_plan_scoping, architecture, stderr, logging, wontfix]
retrievals: 0
status: active
type: clarifications
---

# In ATCR, do PrepareReview or PrepareReviewFromDiff contain r

## Decision

No. There are zero raw os.Stderr writes inside PrepareReview or PrepareReviewFromDiff. The sprint-plan truncation/scope-constraint warnings route through log.FromContext(ctx).Warn (structured logger). resolveScopeConstraint is pure (no I/O) and returns ("","") silently when SprintPlanPath is unset. The --force backup/no-op stderr writes (review.go:295, 312, 321) are gated on Force=true and do not fire in the benchmark loop. review.go:236 is an error return (`if empty { return nil, fmt.Errorf(...) }`), not a stderr write. TD findings citing "stderr noise in loop" at this line should be closed as wontfix.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go
- internal/payload/sprintplan.go
- cmd/atcr/benchmark_run.go

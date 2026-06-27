---
id: mem-2026-06-27-6064c2
question: "What byte ceiling should cap an injected sprint-plan block prepended to every agent prompt in ATCR?"
created: 2026-06-27
last_retrieved: ""
sprints: []
files: [internal/registry/project.go, internal/payload/scope.go, internal/payload/budget.go, internal/fanout/review.go]
tags: [clarifications, epic-12.2_sprint_plan_scoping, implementation, prompt-injection, byte-budget, sprint-plan-scoping]
retrievals: 0
status: active
type: clarifications/epic-12.2_sprint_plan_scoping
---

# What byte ceiling should cap an injected sprint-plan block p

## Decision

16 KiB (16384 bytes). DefaultPayloadByteBudget = 524288 bytes (internal/registry/project.go:25), so 16 KiB is 3.1% of budget — satisfies AC6 without meaningful prompt inflation. No legacy cap exists to match: ScopeRule and ScopeFocus both carry uncapped operator text (scope.go:26 explicitly defers any length bound to a future concern). Truncation at a UTF-8-safe boundary with a one-line stderr note is the correct approach, consistent with the AC3 stderr pattern.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/registry/project.go
- internal/payload/scope.go
- internal/payload/budget.go
- internal/fanout/review.go

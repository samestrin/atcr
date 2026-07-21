---
id: mem-2026-07-20-063fcd
question: "Why does withinSeverityCeiling fail open on an empty/unknown finding severity, and is that documented anywhere co-located with the code?"
created: 2026-07-20
last_retrieved: ""
sprints: [32.1_multi_tier_fix_execution]
files: [internal/verify/severity.go, internal/verify/severity_test.go, internal/verify/executor.go]
tags: [clarifications, sprint-32.1_multi_tier_fix_execution, testing, severity-ceiling]
retrievals: 0
status: active
type: clarifications
---

# Why does withinSeverityCeiling fail open on an empty/unknown

## Decision

withinSeverityCeiling (internal/verify/severity.go:44-56) deliberately fails OPEN when the finding's severity is empty/unknown (rank 0) — the opposite of its sibling meetsSeverityFloor, which fails CLOSED on rank 0. This is safe only because of an ordering contract: meetsSeverityFloor already runs first in the executor's skip chain and skips any empty/unknown-severity finding before withinSeverityCeiling is ever reached, so the ceiling predicate never has to re-decide an ambiguous severity. The contract used to live only in a docstring at the call site; it is now co-located directly on the predicate's docstring (severity.go:44-56) and pinned by a test case with an explanatory comment at internal/verify/severity_test.go:80-85 (`withinSeverityCeiling("","HIGH") -> true`). If this predicate is ever reused standalone (without the floor gate running first), this fail-open behavior needs re-verification — it is only safe under the current call ordering in internal/verify/executor.go's generateFixes skip chain.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/severity.go
- internal/verify/severity_test.go
- internal/verify/executor.go

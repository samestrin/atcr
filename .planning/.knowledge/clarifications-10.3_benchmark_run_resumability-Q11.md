---
id: mem-2026-06-25-44e663
question: "Test helper struct with unprotected int field (concurrent access): is test-only atomic.Int32 fix sufficient or does production code also need changing?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run_resume_test.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, testing, concurrency, golang-atomic]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Test helper struct with unprotected int field (concurrent ac

## Decision

Test-only fix is sufficient when the defective type (e.g. countingCompleter) lives exclusively in the test file and has no production counterpart. Switching from plain int to atomic.Int32 on a test-helper struct is a test-only change with no production implication — the production code path has no analogous shared mutable counter. Pattern: unprotected int on a test-only struct → switch to sync/atomic.Int32 in the test file; no production code requires a parallel change.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run_resume_test.go

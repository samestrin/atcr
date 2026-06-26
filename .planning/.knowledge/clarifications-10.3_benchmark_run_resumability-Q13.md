---
id: mem-2026-06-25-574dad
question: "Vacuous test assertion (checking an unrelated path/directory): is test-only fix sufficient or does production also need changing?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run_resume_test.go, cmd/atcr/benchmark_run.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, testing, test-quality, vacuous-assertion]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Vacuous test assertion (checking an unrelated path/directory

## Decision

Test-only fix is sufficient when the production code already correctly implements the behavior the assertion is meant to verify, but the original assertion was vacuous (e.g. checking an unrelated directory that the production function never writes to). The fix — pointing the candidate path to where the production code could actually write, then asserting absence — is purely test-side. Pattern: vacuous assertion that always passes regardless of production behavior → fix by constructing a concrete candidate path inside the test's temp dir and asserting fs.ErrNotExist after the relevant call. No production code change required if the production gate was already correct.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run_resume_test.go
- cmd/atcr/benchmark_run.go

---
id: mem-2026-06-25-169048
question: "Is test-only sufficient when loadCheckpoint wraps validateCheckpointIntegrity errors with %w — does errors.Is traverse both error layers?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_checkpoint.go, cmd/atcr/benchmark_checkpoint_test.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, testing, error-handling, golang-errors]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Is test-only sufficient when loadCheckpoint wraps validateCh

## Decision

Yes, test-only is sufficient. When loadCheckpoint (cmd/atcr/benchmark_checkpoint.go:103-105) calls validateCheckpointIntegrity and re-wraps its error with %w, errors.Is(err, errCheckpointCorrupt) traverses both wrapping layers correctly. The production code was already correct before the test was added — the test (TestLoadCheckpoint_IntegrityErrors) documents and verifies already-correct behavior covering all three corruption cases (duplicate index, negative index, empty case ID). No production code change is needed when the error chain uses %w at each layer.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_checkpoint.go
- cmd/atcr/benchmark_checkpoint_test.go

---
id: mem-2026-06-25-bdec32
question: "Checkpoint-save failure after in-memory case append: should the intended recovery be persist-before-append or accept re-execution of the failed case?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run.go, cmd/atcr/benchmark_checkpoint.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, architecture, checkpoint-atomicity, save-failure, AC1]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Checkpoint-save failure after in-memory case append: should 

## Decision

Accept re-execution on save failure. The checkpoint guarantee is scoped to cases 1..N-1 (already completed cases), not case N itself. The "out of scope" decision for auto-retry/backoff means case N is the operator's responsibility to re-execute. The on-disk checkpoint at N-1 is the correct recovery position — a failed saveCheckpoint for case N leaves the checkpoint file intact at N-1, so the next operator-initiated run re-executes case N from scratch. Persist-before-append (writing to disk before folding into the in-memory accumulator) adds ordering complexity that the ACs do not require and the epic explicitly scopes out.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run.go
- cmd/atcr/benchmark_checkpoint.go

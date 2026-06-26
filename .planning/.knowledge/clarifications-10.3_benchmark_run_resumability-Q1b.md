---
id: mem-2026-06-25-35477e
question: "Checkpoint struct field removal backward compatibility: if a field is dropped from a checkpoint struct, do old checkpoint files remain loadable?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_checkpoint.go, cmd/atcr/benchmark_run.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, architecture, checkpoint, golang-json, backward-compat]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Checkpoint struct field removal backward compatibility: if a

## Decision

Yes, old checkpoints remain loadable without migration. Go's json.Unmarshal silently discards JSON keys that have no corresponding struct field — so a checkpoint file written with an "expected" field inside checkpointReviewer will still parse correctly after the field is removed from the struct. The dropped key is ignored, no error is returned, and no version bump or migration path is required. This applies to any field removal from a checkpoint/resume struct in Go so long as the remaining fields are still correctly populated. Pattern: prefer dropping redundant fields (re-derivable from the source-of-truth at replay time) over keeping them in the checkpoint; backward compat is free in Go JSON unmarshaling.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_checkpoint.go
- cmd/atcr/benchmark_run.go

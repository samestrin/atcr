---
id: mem-2026-06-25-c8d782
question: "Reviewer identity model resolution timing for byte-identical AC3: first-case-wins vs deferred two-pass — which is correct?"
created: 2026-06-25
last_retrieved: ""
sprints: []
files: [cmd/atcr/benchmark_run.go, cmd/atcr/benchmark_checkpoint.go]
tags: [clarifications, epic-10.3_benchmark_run_resumability, architecture, AC3, reviewer-identity, model-resolution]
retrievals: 0
status: active
type: clarifications skill — epic 10.3_benchmark_run_resumability, 2026-06-25
---

# Reviewer identity model resolution timing for byte-identical

## Decision

First-case-wins is correct for AC3 (byte-identical resume). The pattern: reviewerModel() prefers provider-reported model, falls back to configured model when usage is not reported; applyReviewerOutcome locks identity on first sighting of each reviewer (first-case-wins); replayCheckpointCase routes checkpointed entries through the same applyReviewerOutcome path, reconstructing the identical acc.model from the stored (already-resolved) model value. Deferred two-pass strategies break streaming per-case checkpoint writes (AC1 requires writing after each case before accumulation is complete). "First usage-reporting case" strategies introduce mutable identity state that diverges between fresh and resumed runs, breaking AC3 determinism. Pattern: lock reviewer identity on first sighting; store the already-resolved model in the checkpoint (not a placeholder requiring re-resolution); replay through the same identity-lock path.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- cmd/atcr/benchmark_run.go
- cmd/atcr/benchmark_checkpoint.go

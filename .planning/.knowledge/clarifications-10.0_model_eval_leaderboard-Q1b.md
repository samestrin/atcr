---
id: mem-2026-06-24-2f0567
question: "When should `atcr benchmark run` execution be deferred vs. built as part of the `atcr benchmark` command?"
created: 2026-06-24
last_retrieved: ""
sprints: []
files: [internal/fanout/review.go:516, cmd/atcr/review.go:197, cmd/atcr/review.go:244]
tags: [clarifications, epic-10.0_model_eval_leaderboard, scope, architecture, benchmark]
retrievals: 0
status: active
type: clarifications skill — epic mode, 2026-06-24
---

# When should `atcr benchmark run` execution be deferred vs. b

## Decision

`benchmark run` (live execution + scoring) must be deferred whenever: (1) the review pipeline ingests git refs via `buildPayloads(repo, base, head)` — not loose diff files — so a new diff-file→payload ingestion path is required before benchmark cases can be reviewed; (2) `ExecuteReview` calls `llmclient.New()` directly, making execution network-bound and non-unit-testable without the full pipeline machinery; (3) the scoring rubric (planted-defect expected categories) lives in the external benchmark-suite repo and is not yet present. The bounded, testable in-repo pieces — suite-manifest contract (Load/Validate + reproducibility hash), `atcr benchmark verify --suite-path`, and `atcr benchmark export` (suite-tagged record distinct from production --export) — should ship first as Epic 10.0's T2 delivery. `benchmark run` lands in Epic 10.1 alongside the diff-file ingestion path and T3 suite content.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/review.go:516
- cmd/atcr/review.go:197
- cmd/atcr/review.go:244

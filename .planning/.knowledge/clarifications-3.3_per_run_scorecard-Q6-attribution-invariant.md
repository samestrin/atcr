---
id: mem-2026-06-15-75bc0c
question: "How is the winning-model attribution in internal/verify guaranteed to follow the winning verdict rather than defaulting to the first skeptic (skeptics[0])?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/verify/pipeline.go, internal/verify/pipeline_test.go, internal/verify/votes.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, verify-pipeline, model-attribution, testing-invariant]
retrievals: 0
status: active
type: clarifications
---

# How is the winning-model attribution in internal/verify guar

## Decision

winningAttribution (internal/verify/pipeline.go:478-518) computes a `decisive` flag, then for a decisive winner skips losing voters (`if decisive && v.Verdict != winner { continue }`) and records only the winning verdict's skeptics' models in selection order, deduped — it is never a blind skeptics[0..n]. The invariant is locked by a pair of tests: (1) the exact-equality assertion `assert.Equal(t, "m-s2, m-skep", vf.Findings[0].Model)` (internal/verify/pipeline_test.go:310, commit 1b1f6f0) pins selection order, dedup, and absence of extras — strictly stronger than two `assert.Contains` substring checks; and (2) TestRunVerify_WinningModelAttribution_TwoRefuteOneConfirm (internal/verify/pipeline_test.go:807-840, commit 6b47c7d) forces the majority verdict onto the REFUTED side while making the CONFIRMING skeptics[0] (m-s2, first alphabetically) a controlled loser, with a `NotContains(m-s2)` assertion that a naive "return skeptics[0]" implementation would fail. The confirm-wins polarity is independently covered by TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute. Aggregation backing: aggregateVerdicts gives strict-majority (2-refute > 1-confirm → refuted, not a tie) at internal/verify/votes.go:46-72.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
- internal/verify/pipeline_test.go
- internal/verify/votes.go

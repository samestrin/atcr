---
id: mem-2026-06-15-062c4c
question: "In internal/verify, does winningAttribution (pipeline.go) need to filter to canonical verdicts to stay consistent with aggregateVerdicts (votes.go)?"
created: 2026-06-15
last_retrieved: ""
sprints: [3.3_per_run_scorecard]
files: [internal/verify/pipeline.go, internal/verify/votes.go, internal/verify/verdict.go, internal/verify/invoke.go]
tags: [clarifications, sprint-3.3_per_run_scorecard, architecture, verify-pipeline, skeptic-verdicts, defensive-code]
retrievals: 0
status: active
type: clarifications
---

# In internal/verify, does winningAttribution (pipeline.go) ne

## Decision

No functional change is required — the divergence is unreachable today (defensive-only). All verdicts that reach winningAttribution are already canonical: parseVerdict maps any unknown verdict to `unverifiable` (internal/verify/verdict.go:60-68 default branch), and every invokeSkeptic exit plus the verifyFinding error fallback hardcode canonical verdicts (internal/verify/invoke.go:67,79,84; internal/verify/pipeline.go:438). winningAttribution and aggregateVerdicts consume the SAME perSkeptic slice with equivalent strict-plurality logic (winner picked by strict max; ties→unverifiable), so on canonical input they cannot disagree. The only real asymmetry: aggregateVerdicts skips non-canonical verdicts in its valid-filter switch (votes.go:31-34) while winningAttribution counts them unconditionally (pipeline.go:482-486) — but since no producer can emit a non-canonical verdict, that branch is unreachable. The in-scope ~12-line canonical filter and the cross-file aggregateVerdicts signature change both alter zero observable behavior, so both violate minimum-code; the in-scope filter also adds an untestable branch. The only justified edit is tightening the comment at pipeline.go:478-480 to document the canonical-input precondition so a future verdict producer knows to revisit the filter.

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/verify/pipeline.go
- internal/verify/votes.go
- internal/verify/verdict.go
- internal/verify/invoke.go

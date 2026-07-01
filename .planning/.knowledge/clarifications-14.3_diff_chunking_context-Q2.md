---
id: mem-2026-07-01-42b113
question: "Signal partial-chunk-coverage via additive Result field, not a new Status enum"
created: 2026-07-01
last_retrieved: ""
sprints: []
files: [internal/fanout/engine.go, internal/fanout/artifacts.go, internal/fanout/chunker.go, internal/fanout/status.go]
tags: [clarifications, epic-14.3_diff_chunking_context, implementation, fanout]
retrievals: 0
status: active
type: clarifications
---

# Signal partial-chunk-coverage via additive Result field, not

## Decision

Use an additive, machine-readable field (e.g. PartialChunks/UnreviewedFileCount) on Result (internal/fanout/engine.go) and thread it through statusFor/AgentStatus into summary.json (internal/fanout/artifacts.go), leaving Result.Status = StatusOK unchanged for a partial-chunk persona. Do NOT introduce a new StatusPartial value — status.go's 3-value const block (StatusOK/Failed/Timeout) is branched on at 12+ call sites across fanout/reconcile (outcome.go summarize(), resume.go resume-completion checks, metrics.go), and most would silently treat a new status as a failure, causing spurious re-runs/exclusions of personas that did contribute real findings.

Precedent in this codebase: PoolSummary.GroundingEnabled *bool / GroundingDisabledReason string (internal/fanout/artifacts.go:47-57) is the established pattern for exposing a non-obvious run condition in summary.json via an additive, nullable field rather than extending the Status enum.

General pattern for this codebase: when a fan-out result needs to expose a partial/degraded condition (as opposed to a simple success/failure), add an additive field to Result/summary.json following the GroundingEnabled precedent, rather than growing the Status enum — the Status enum's small value set is relied upon by many downstream branches (resume, metrics, consensus) that treat "not StatusOK" as failure.

- internal/fanout/engine.go:152-189 (Result struct)
- internal/fanout/artifacts.go:31-58,245-279 (statusFor/AgentStatus/PoolSummary — the summary.json write path)
- internal/fanout/chunker.go:261-268 (mergeResultGroup already computes okCount > 0 && okCount < len(g) but only logs it to stderr)
- internal/fanout/status.go:26-28 (3-value Status const, doc comment enumerates only OK/Failed/Timeout)

## Rationale

- [from context]

## Applies When

- [conditions]

## Code Reference

- internal/fanout/engine.go
- internal/fanout/artifacts.go
- internal/fanout/chunker.go
- internal/fanout/status.go

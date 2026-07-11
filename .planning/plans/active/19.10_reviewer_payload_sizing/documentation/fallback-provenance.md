# Fallback Provenance (F5)

**Priority: IMPORTANT**

## Overview

When overflow forces a model swap via litellm's `context_window_fallbacks`, the substitution must be recorded in `summary.json` so `reconcile`'s distinct-reviewer CONFIDENCE calculus is never silently inflated by a fallback model standing in for multiple original reviewers.

> Source: [plan.md](../plan.md):Objectives:F5, [original-requirements.md](../original-requirements.md):Requirements:F5

Heterogeneity is the panel's value. Same-model degradation (`chunk`, `truncate`) is preferred; any fallback swap is recorded and de-weighted.

> Source: [original-requirements.md](../original-requirements.md):Non-Functional:Provenance integrity

## Key Concepts

- **Why provenance matters.** litellm fallback can route any persona to any fallback model — for example, a single universal high-context net model backing every persona. Without provenance, `reconcile` would count that one model as multiple distinct reviewers, inflating confidence.

  > Source: [original-requirements.md](../original-requirements.md):Technical Approach:Fallback semantics

- **`AgentStatus` already carries fallback fields.** `FallbackUsed` and `FallbackFrom` exist today in `internal/fanout/status.go:286-295`. The multi-chunk merge path already unions `FallbackFrom` across chunks (comma-joined, sorted) in `mergeResultGroup`.

  > Source: `internal/fanout/status.go:286-295`, `internal/fanout/chunker.go:261-268`

- **Single-chunk (bulk) path needs new plumbing.** The chunked path already aggregates fallback provenance; the bulk/single-call path must write the same fields into the `Result` that `statusFor` consumes.

  > Source: codebase-discovery.json:reusable_components:mergeResultGroup fallbackFromSet aggregation

- **`reconcile` must become fallback-aware.** The distinct-reviewer independence model lives in `internal/reconcile/disagree.go` (`BuildDisagreements`, `IndependenceModelReviewerCount`), and the wire record is `JSONFinding` in `internal/reconcile/emit.go`. A slot served by a fallback model must not be counted as an additional distinct model voice for the original reviewer.

  > Source: codebase-discovery.json:semantic_matches:IndependenceModelReviewerCount / BuildDisagreements, codebase-discovery.json:semantic_matches:JSONFinding.Reviewers / Confidence

## Implementation Guidance

- Surface fallback provenance at the run level in `internal/fanout/artifacts.go` `writePool` / `summarize` (`summarize` is defined in `internal/fanout/outcome.go:49` and called from `artifacts.go`) and per-agent in `internal/fanout/artifacts.go` `statusFor` (`statusFor` is in `artifacts.go:275`, not `status.go`).

  > Source: `internal/fanout/artifacts.go:93-121` (`writePool`) / `internal/fanout/artifacts.go:275` (`statusFor`)

- Add a provenance field to the reconcile wire types: `stream.Finding` (`internal/stream/parser.go:46`, the record reconcile consumes) and the emitted `JSONFinding` (`internal/reconcile/emit.go:62`), threading it through `internal/reconcile/adapter/adapter.go`. (There is no distinct `reconcile.Finding` type — the reconcile package operates on `stream.Finding` and emits `JSONFinding`.)

  > Source: codebase-discovery.json:integration_gaps:Reconcile fallback-provenance wire type; `internal/stream/parser.go:46`, `internal/reconcile/emit.go:62`

- In `internal/reconcile/disagree.go`, treat fallback-served sources as non-distinct for the original model when computing independence/confidence.

  > Source: codebase-discovery.json:files_to_modify:internal/reconcile/disagree.go

- In `internal/reconcile/emit.go`, consume the provenance when building `JSONFinding` records so downstream confidence/radar values are not inflated.

  > Source: codebase-discovery.json:files_to_modify:internal/reconcile/emit.go

## Integration Gap

`stream.Finding` (`internal/stream/parser.go:46`, the record reconcile consumes) and the emitted `JSONFinding` (`internal/reconcile/emit.go:62`) currently carry no model/fallback-provenance field. F5 cannot be fully implemented until a field is added and threaded through `internal/reconcile/lib.go` and `internal/reconcile/adapter/adapter.go`. (No separate `reconcile.Finding` type exists; do not add a field to a type of that name.)

> Source: codebase-discovery.json:integration_gaps:Reconcile fallback-provenance wire type; `internal/stream/parser.go:46`, `internal/reconcile/emit.go:62`

## Related Documentation

- [on_overflow Policy](on-overflow-policy.md) — the `fallback` arm depends on this work (F4)
- [Diagnosability Fields](diagnosability-fields.md) — records fallback substitution per agent (F8)
- `internal/fanout/chunker.go` — existing `FallbackFrom` union pattern
- `internal/fanout/status.go` — `AgentStatus.FallbackUsed` / `FallbackFrom`
- `codebase-discovery.json` — discovery findings for F5

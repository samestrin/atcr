# Plan 19.10: Per-Model Payload Sizing & Graceful Degradation

## Metadata
- **Plan Type:** bugfix
- **Last Modified:** 2026-07-11
- **Original Requirements:** [original-requirements.md](original-requirements.md)

## Plan Overview
**Plan Goal:** Size each multi-agent reviewer's payload to its own model's token window (reserving the output-token budget), and when a payload still doesn't fit, chunk it to fit using the existing Epic 14.3 chunker made window-aware — degrading gracefully via a configurable `on_overflow` policy instead of silently shedding files identically across a heterogeneous 32k→144k-token roster.
**Target Users:** atcr maintainers/operators running the multi-agent reviewer against large diffs/sprints; the reviewer's own heterogeneous roster of LLM agents (`dax`, `otto`, `greta`, `vera`, `brad`, and others spanning small- to large-context models).
**Framework/Technology:** Go 1.25 stdlib; existing `internal/payload` (byte-budget shedding, sprint-plan capping), `internal/fanout` (dispatch, chunking, caching, artifacts), `internal/registry` (config/settings resolution), `internal/cache` (content-addressed diff cache), `internal/reconcile` (distinct-reviewer confidence). No new third-party dependency — the byte→token conversion is a conservative static ratio, not a live tokenizer.

## Objectives
1. Replace the single global byte budget applied identically to every agent (`internal/payload/budget.go` `ApplyByteBudget`, called at `internal/fanout/review.go:464`/`:726`) with a per-model context-window resolver (F1) and an effective per-agent input budget that reserves the output-token cap (F2).
2. Make the existing Epic 14.3 chunker (`internal/fanout/chunker.go` `chunkDiff`) window-aware so overflow is delivered whole across appropriately-sized chunks per model rather than shed (F3), reusing its existing 64-chunk/agent ceiling.
3. Ship `on_overflow` as a real, configurable degradation policy (`chunk` default, `truncate`, `fallback` recognized, `fail` recognized) rather than a hardcoded shed (F4).
4. Record fallback-model provenance in `summary.json` so `reconcile`'s distinct-reviewer CONFIDENCE calculus is never inflated by a silent model swap (F5).
5. Scale the per-agent request timeout with payload size/chunk count (or a raised ceiling) so a small-window model fanned into many chunks on a slow local backend no longer hits the 600s wall (F6).
6. Fold the per-agent effective budget/chunk plan into the fan-out cache key so a per-agent-sized payload is never served a stale full-payload cache hit (F7).
7. Extend `summary.json`/`AgentStatus` with per-agent diagnosability fields: effective input budget, resolved model window, reserved output tokens, chunk count, and degradation action taken (F8).
8. Replace the hardcoded `MaxSprintPlanBytes` (16 KB) constant in `internal/payload/sprintplan.go` with a configurable `max_sprint_plan_bytes` (default 65536/64 KB) parsed from `.atcr/config.yaml` (F9).
9. Provide a scripted, standalone-runnable live audit harness that re-runs the exact 19.6 range against the real roster on `orchestrator.lan`, hard-gated on zero `ContextWindowExceededError` and all five previously-failing agents completing `status=ok`.

## Scope
### In Scope
- `internal/payload`: new per-model context-window resolver (F1), conservative byte→token conversion, sprint-plan byte-limit configurability (F9).
- `internal/fanout`: per-agent effective-budget + chunk-plan derivation at dispatch (F2), window-aware `chunker.go` wiring (F3), `on_overflow` policy dispatch (F4), timeout scaling (F6), cache-key correctness in `cache.go` (F7), diagnosability fields in `artifacts.go`/`status.go` (F8).
- `internal/reconcile`: fallback-provenance de-weighting in the distinct-reviewer CONFIDENCE calculus only (F5).
- `internal/registry`: `max_sprint_plan_bytes` config schema + resolver only (F9) — no other timeout/budget resolver changes.
- A scripted, env-coupled live audit harness (not a `go test`) re-running the 19.6 range against `orchestrator.lan`.

### Out of Scope
- Live/dynamic context-window resolution from provider `model_info`/catalog endpoints (belongs to Epic 19.7; this plan ships a static, deterministic table).
- A broader pseudo-deterministic prompt-cache-for-determinism layer (orthogonal to sizing; `cache.go` is touched only for key correctness).
- A config-schema per-model window field on `Provider`/agent config (deferred; the static table with a conservative default stands alone).
- Reconciler/severity-gate behavior and wording beyond the fallback-provenance de-weighting (F5).

## Dependencies and Context
- **19.6 Community Registry Hub** (completed): the confirmed failure run this plan fixes — a 101-file/6,429-insertion diff returned 1 finding from 11 reviewers (5 ok, 3 timeout, 3 failed) against the current global-byte-budget design.
- **Epic 14.3 (Context-Aware Diff Chunking)** (completed): supplies the `chunkDiff`/`mergeChunkResults` primitives this plan makes window-aware; not reimplemented, only re-parameterized.
- **Epic 5.2 (Diff Caching & Incremental Reviews)** (completed): supplies the `internal/cache` content-addressed key this plan extends for per-agent-sized payload correctness (F7).
- **Epic 19.7 (Live Model Resolution)**: soft, non-blocking future source of live context-window values for F1's static table; no hard dependency either direction.
- **Epic 12.2 (Sprint Plan Scoping)** (completed): owns the existing `MaxSprintPlanBytes`/`ScopeConstraint` mechanism this plan makes configurable (F9).

## Planning Deliverables
### Tasks
- **Location:** [`tasks/`](tasks/)
- **Status:** Generated
- **Estimated Count:** ~12 tasks across the 10 implementation components identified in the original requirements

| # | Task | File |
|---|------|------|
| 1 | Per-Model Context-Window Resolver (F1) | [`tasks/task-01-context-window-resolver.md`](tasks/task-01-context-window-resolver.md) |
| 2 | Per-Agent Effective Input Budget (F2) | [`tasks/task-02-per-agent-effective-budget.md`](tasks/task-02-per-agent-effective-budget.md) |
| 3 | Window-Aware Chunking (F3) | [`tasks/task-03-window-aware-chunking.md`](tasks/task-03-window-aware-chunking.md) |
| 4 | `on_overflow` Policy Dispatch (F4) | [`tasks/task-04-on-overflow-policy-dispatch.md`](tasks/task-04-on-overflow-policy-dispatch.md) |
| 5 | `on_overflow` Config Schema (F4) | [`tasks/task-05-on-overflow-config-schema.md`](tasks/task-05-on-overflow-config-schema.md) |
| 6 | Fallback Provenance — Fanout (F5) | [`tasks/task-06-fallback-provenance-fanout.md`](tasks/task-06-fallback-provenance-fanout.md) |
| 7 | Reconcile Fallback-Aware De-Weighting (F5) | [`tasks/task-07-reconcile-fallback-deweighting.md`](tasks/task-07-reconcile-fallback-deweighting.md) |
| 8 | Timeout Scaling with Chunk Count / Payload Load (F6) | [`tasks/task-08-timeout-scaling.md`](tasks/task-08-timeout-scaling.md) |
| 9 | Cache-Key Correctness (F7) | [`tasks/task-09-cache-key-correctness.md`](tasks/task-09-cache-key-correctness.md) |
| 10 | Diagnosability Fields (F8) | [`tasks/task-10-diagnosability-fields.md`](tasks/task-10-diagnosability-fields.md) |
| 11 | Configurable Sprint-Plan Limit (F9) | [`tasks/task-11-configurable-sprint-plan-limit.md`](tasks/task-11-configurable-sprint-plan-limit.md) |
| 12 | Live Audit Harness (AC-Live) | [`tasks/task-12-live-audit-harness.md`](tasks/task-12-live-audit-harness.md) |

## Technical Debt Analysis Summary
The payload sizer has no per-model token awareness, producing four independent, all-confirmed failure axes: (1) one global byte budget (`payload_byte_budget`, default 512 KiB) applied identically to every agent regardless of its model's actual context window, so every agent drops the same files; (2) the 8192-token output cap (`defaultMaxTokens`) is never reserved against the input-sizing calculation, causing exact-boundary overflows (the confirmed `dax` case: 24577 input tokens = 32768 − 8192 + 1); (3) no graceful degradation — litellm `context_window_fallbacks` is unset, so overflow kills the agent outright instead of falling back or truncating; (4) the 600s timeout wall is too tight for slow local backends, worsened by oversized payloads (`greta`/`vera`/`brad` all hit `context deadline exceeded`). A latent fifth issue: the Epic 14.3 chunked path bin-packs by diff line count (`MaxContextLines`, default 1500), not by token window, so it can still overflow a 32k model or under-utilize a 144k model.

## Technical Planning Notes
- **Existing Pattern — non-silent degradation record:** `Truncation{Truncated, FilesDropped, AllDropped}` (`internal/payload/budget.go`) is always returned, never a silent side effect; `AgentStatus`/`PoolSummary` follow the same convention. F4/F8 must extend this pattern, not replace it.
- **Existing Pattern — pointer-for-unset-vs-explicit:** `AgentConfig.MaxContextLines *int`, `TimeoutSecs *int` with `Effective*()` resolvers is the established precedence-chain shape for **per-agent/per-executor** overrides. For project/registry-level numeric settings with no per-agent dimension, the established shape is a `*int64` on `ProjectConfig`/`Registry` resolved into a plain field on `Settings` by `ResolveSettings` (see `PayloadByteBudget` and `CacheMaxBytes`). The new `max_sprint_plan_bytes` (F9) follows this latter shape; it needs no standalone `Effective*()` identity wrapper because there is nothing to resolve at that layer.
- **Existing Pattern — NUL-separated composite cache keys:** `cache.Key`/`diffCacheKey` join heterogeneous components (promptHash, model, baseURL, temperature) with a NUL byte before hashing specifically to avoid boundary-ambiguity collisions. F7 folds the effective budget/chunk-plan value into this same tuning token.
- **Existing Pattern — chunk-result merge-by-persona:** `mergeChunkResults`/`mergeResultGroup` (`internal/fanout/chunker.go`) already aggregate `FallbackFrom` across chunks (comma-joined, sorted) for a multi-chunk persona. F5's fallback provenance reuses this exact aggregation; only the single-chunk (bulk) path needs new plumbing into `AgentStatus`.
- **Both degradation primitives already exist**: `ApplyByteBudget` (shed) and `chunkDiff` (split) are both implemented today. F3 is a wiring change — feed `chunkDiff` a per-model token-derived `maxLines` — not a new chunking algorithm.
- **Byte→token ratio anchor**: `internal/registry/config.go`'s `DefaultMaxContextLines` doc comment already estimates "1500 lines ≈ 22k–27k tokens... under the 128k `DefaultPayloadByteBudget`," implying an existing ~4.1 B/token assumption at `internal/registry/project.go:89`. The plan's NFR explicitly requires the more conservative ~3.5 B/token (over-reserve) instead.
- **Integration points**: `internal/fanout/review.go:464`/`:726` (the two identical global-budget call sites), `:948-989` (`defaultMaxTokens`/`diffCacheKey`), `internal/fanout/engine.go:610` and `review.go:516` (timeout/deadline application, read `EffectiveTimeoutSecs` — do not modify the resolver), `internal/fanout/artifacts.go` `writePool` / `internal/fanout/status.go` `statusFor` (where F5/F8 fields attach).

## Documentation References

### Critical (read before coding)
- **[CRITICAL]** [Context-Window Resolver](documentation/context-window-resolver.md) — static per-model context-window table for F1
- **[CRITICAL]** [Per-Agent Budget & Chunking](documentation/per-agent-budget-and-chunking.md) — output-reserved per-agent input budget and window-aware chunk plan for F2/F3
- **[CRITICAL]** [on_overflow Policy](documentation/on-overflow-policy.md) — degradation ladder (`chunk`, `truncate`, `fallback`, `fail`) and config surface for F4

### Important (review during development)
- **[IMPORTANT]** [Cache-Key Correctness](documentation/cache-key-correctness.md) — folding effective budget / chunk plan into the diff-cache key for F7
- **[IMPORTANT]** [Diagnosability Fields](documentation/diagnosability-fields.md) — per-agent `summary.json` fields for F8
- **[IMPORTANT]** [Fallback Provenance](documentation/fallback-provenance.md) — fallback model substitution recording and reconcile de-weighting for F5
- **[IMPORTANT]** [Timeout Scaling](documentation/timeout-scaling.md) — load-scaled request timeout for F6
- **[IMPORTANT]** [Config YAML Parsing (gopkg.in/yaml.v3)](documentation/config-yaml-parsing.md) — how F9 (`max_sprint_plan_bytes`) and F4 (`on_overflow`) should be added to `.atcr/config.yaml`, following the codebase's existing pointer + `Effective*()` resolver convention and yaml.v3's `KnownFields(true)` strict-mode decoding.

No other `.planning/specifications/` documents scored above the relevance threshold for this plan — it is internal Go wiring across `internal/payload`/`internal/fanout`/`internal/registry`/`internal/reconcile` with no dedicated architectural spec. See [documentation/source.md](documentation/source.md) for the full search record.

## Implementation Strategy
Land F1 (context-window resolver) and F2 (output-reserved effective budget) first since every other requirement depends on having a per-model budget to size against — this directly closes the confirmed `dax` boundary-overflow arithmetic (AC1, AC2). Next, make the Epic 14.3 chunker window-aware (F3) by feeding it a per-model `maxLines` derived from the new effective budget, and wire the `on_overflow` policy dispatch (F4) so `chunk` becomes the default overflow path with `truncate` as the explicit fallback and `fallback`/`fail` recognized-but-gated arms. With sizing and chunking in place, add fallback provenance recording (F5) reusing the existing chunk-merge aggregation pattern, then co-design timeout scaling (F6) with the chunking behavior since a small-window model fanned into many chunks on a slow backend is exactly the load pattern that must not re-trigger the 600s wall. Fold the cache-key fix (F7) in alongside F2/F3 since a stale full-payload cache hit is a silent-failure risk the moment per-agent sizing exists. Add diagnosability fields (F8) as each preceding requirement lands its own data point, rather than as a final bolt-on. Ship the configurable sprint-plan limit (F9) as an independent, parallel-track item since it touches only `internal/registry`'s config schema and `internal/payload/sprintplan.go`. Close with the scripted live-audit harness (AC-Live), runnable standalone and invoked in the execution loop, re-running the exact 19.6 range against the real roster.

## Recommended Packages
No high-ROI packages identified. The implementation is pure internal wiring over existing Go stdlib and already-vendored dependencies (`gopkg.in/yaml.v3` for config parsing). The byte→token conversion is an intentionally conservative static ratio (~3.5 B/token + safety margin), not a live tokenizer library — matching the plan's Determinism NFR (no hot-path network/library calls).

## Success Criteria
- The reviewer reviews its own 6,400-line sprint without gutting the panel — findings come from multiple agents, not 1.
- No agent hard-fails on context overflow; degradation is visible in `summary.json`, never silent.
- Panel model-diversity is preserved on the default path (chunk, same model); any fallback swap is recorded.
- The five previously-failing agents from the 19.6 run (`dax`, `otto`, `greta`, `vera`, `brad`) all complete `status=ok` on the exact 19.6 diff range re-run against the real roster.
- `go test ./...` passes.
- The byte→token conversion over-reserves with a conservative ratio (~3.5 B/token plus safety margin), never the optimistic ~4.1 B/token assumption.
- Window resolution, effective-budget derivation, and chunk planning are deterministic from `(entries, model, config)` with no live/network resolution on the hot path.
- `on_overflow: chunk` never drops a file; zero content loss on the default degradation path.
- The `max_sprint_plan_bytes` limit is configurable in `.atcr/config.yaml` and verified by test (AC10).

## Risk Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Byte→token ratio too optimistic → residual overflow | Medium | High | Conservative ratio (~3.5 B/tok) + safety margin; `on_overflow` net catches the tail |
| Chunking a 32k model on a slow backend re-triggers timeouts | Medium | High | F6 timeout scaling designed *with* chunking, not after |
| Cache serves stale full-payload for a per-agent-sized request | Medium | High | F7 cache-key folds effective budget/chunk plan; explicit regression test |
| Fallback silently corrupts reconcile CONFIDENCE | Low | Medium | F5 records every substitution; fallback is opt-in, not default |
| Scope creep into `internal/registry` config schema → larger sprint | Medium | Medium | Hold changes to payload+fanout; read resolved values; escalate explicitly if a schema field is unavoidable |

## Next Steps
1. `/find-documentation @.planning/plans/active/19.10_reviewer_payload_sizing/`
2. `/create-documentation @.planning/plans/active/19.10_reviewer_payload_sizing/`
3. `/create-tasks @.planning/plans/active/19.10_reviewer_payload_sizing/`
4. `/design-sprint @.planning/plans/active/19.10_reviewer_payload_sizing/`
5. `/create-sprint @.planning/plans/active/19.10_reviewer_payload_sizing/`

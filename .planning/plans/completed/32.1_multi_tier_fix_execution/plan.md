# Plan 32.1: Multi-Tier Fix Execution Engine

## Plan Overview
**Plan Type:** feature
**Last Modified:** July 20, 2026 10:44:00AM
**Plan Goal:** Evolve atcr's single-model fix executor into a complexity-aware, ceiling-configurable execution path so cheap/local models handle trivial fixes while expensive frontier models are reserved for complex findings, without re-implementing what the codebase already does. The existing `EstMinutes` estimate — already emitted by every reviewer persona — becomes the routing signal; only the config surface and routing decision are net-new.
**Target Users:** atcr operators on the BYO-Keys architecture who want cost-optimized auto-fix runs (route cheap findings to local/cheap models, reserve frontier models for hard bugs).
**Framework/Technology:** Go, `internal/registry` and `internal/verify` packages

## Objectives

1. **Complexity signal from reviewers (already satisfied — retained for traceability):** Every reviewer finding carries an estimated complexity/time metric. Codebase discovery confirmed this is already true end-to-end (`EST_MINUTES` in `personas/_base.md` → `Finding.EstMinutes` in `internal/stream/parser.go` → `JSONFinding.EstMinutes` in `internal/reconcile/emit.go`), so no new reviewer-prompt or schema work is needed. This objective covers the original epic's AC1/T1 by building on the existing signal rather than recreating it.
2. **Complexity-ceiling configuration:** Extend `ExecutorConfig` (`internal/registry/config.go`) with ceiling options — `max_estimated_minutes` as the primary routing signal, plus an optional `max_severity_for_fix` severity ceiling complementing the existing `min_severity_for_fix` floor — each validated in `validateExecutor` with the same named-constant + explicit-range-check convention as existing fields. (Covers original epic AC2/T2; `max_tool_calls` from the epic's example list already exists.)
3. **Ceiling-aware routing in the fix engine:** `generateFixes` (`internal/verify/executor.go`) skips findings whose `EstMinutes` (or severity) exceeds the configured ceiling as an additional condition in its existing skip chain, leaving them for a subsequent tier/run instead of attempting them. (Covers original epic AC3/T3.)
4. **Self-gating on over-complex fixes:** The executor can self-assess and decline a fix it realizes is too complex, surfacing it as a skipped/declined finding with an explicit reason rather than hallucinating a partial fix. (Covers original epic Proposed Solution #4.)
5. **Skip visibility:** Ceiling-skipped and self-declined findings are explicitly logged so no finding is silently dropped. (Covers original epic T4.)
6. **Docs and worked example:** `docs/registry.md`, `docs/findings-format.md`, and `examples/registry-with-executor.yaml` document the new ceiling fields and demonstrate a two-tier cheap-then-frontier workflow. (Covers original epic AC4/T5.)

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/32.1_multi_tier_fix_execution/`

## Feature Analysis Summary

Codebase discovery (see `codebase-discovery.json`) found that AC1 of the original epic ("Reviewer agents output an estimated complexity/time metric for every finding") is **already satisfied** by existing code: `personas/_base.md` already instructs every reviewer persona to emit an `EST_MINUTES` column, `internal/stream/parser.go` already parses it into `Finding.EstMinutes`, and `internal/reconcile/emit.go` already carries it through to `JSONFinding.EstMinutes` (`json:"est_minutes"`) in `findings.json`. This changes the plan's real scope: no new reviewer-prompt or schema work is needed to produce a complexity signal — the work is building a routing decision on top of the signal that already exists.

The remaining scope is the config surface (a complexity ceiling on the executor), the routing/skip logic in `generateFixes`, skip visibility (logging), and documentation. One genuine open design question remains: whether "multi-tier" means a single executor with a ceiling that causes over-ceiling findings to be skipped-and-logged (small, additive change; a second tier is then a second independently-configured run against the same `findings.json`), or a true in-process ordered chain of executors that atcr walks automatically within one run (larger schema change to `Registry.Executor`, which is a single pointer field today). The original epic's own AC4 language ("a second run (or secondary tier)") suggests the former was already anticipated as acceptable. This should be locked down as a clarification before `/design-sprint` fixes the phase structure.

## Technical Planning Notes

- `ExecutorConfig` (`internal/registry/config.go:206-225`) is single-executor today; `Registry.Executor` is a `*ExecutorConfig`, not a list — adding true multi-executor chaining is a schema-breaking-adjacent change, while adding a ceiling field to the existing struct is additive.
- `generateFixes` (`internal/verify/executor.go:104-232`) already runs a per-finding skip chain (confidence ≥ HIGH, severity ≥ floor, not already fix-attributed) before dispatch — a complexity-ceiling check is a natural fourth condition in that same chain, not a new subsystem.
- `JSONFinding.EstMinutes` (`internal/reconcile/emit.go:69`) is already populated end-to-end from reviewer output and merged via MAX across duplicate findings (`internal/reconcile/merge.go:65`) — route on this field; do not invent a parallel `complexity_score`.
- `validateExecutor` (`internal/registry/config.go:593-677`) follows a consistent named-constant + explicit-range-check convention per field — new ceiling fields must match this style, including their own `EffectiveXxx()` resolver method (see `EffectiveFixMinSeverity`, `EffectiveMaxToolCalls` for precedent).
- The original epic plan cited `internal/personas/community_schema.go` and `internal/fanout/engine.go` for T1/T3 — both paths are stale (confirmed by `/refine-epic` on 2026-07-17); the real files are `internal/stream/parser.go`, `internal/reconcile/emit.go`, `personas/_base.md`, and `internal/verify/executor.go` respectively.

## Implementation Strategy

Extend `ExecutorConfig` with complexity-ceiling fields (`max_estimated_minutes` as the primary routing signal, plus an optional `max_severity_for_fix` severity ceiling complementing the existing `min_severity_for_fix` floor), extend `validateExecutor` with matching range validation for each new field, and extend `generateFixes`'s existing skip chain with a ceiling check (paired with a `meetsSeverityFloor`-style helper in `internal/verify/severity.go`) plus explicit skip logging so ceiling-skipped findings remain visible rather than silently dropped. Cover self-gating (original epic Proposed Solution #4) in the same execution path: when the executor self-assesses a fix as too complex and declines, record it as a skipped/declined finding with an explicit reason in logs/output — never a silent drop and never a partial fix presented as complete. Document the new fields in `docs/registry.md` and add a worked two-tier example to `examples/registry-with-executor.yaml`. If the clarification above confirms true in-process multi-executor chaining is required (not just ceiling-skip + a second independently-configured run), that is materially larger scope — a `Registry.Executor` schema redesign — and should be split into its own story with its own risk analysis rather than folded into the ceiling-field work.

## Recommended Packages

No high-ROI packages identified — this is internal routing/config logic against the existing standard library and already-vendored dependencies; no new external dependency closes a gap here.

## User Story Themes

1. **Configure a complexity ceiling** — As an atcr operator, I want to set a `max_estimated_minutes` ceiling on my executor so cheap/local models aren't attempted on findings likely beyond their capability.
2. **Skip over-ceiling findings safely** — As an atcr operator, I want the fix engine to skip (not crash or hallucinate a partial fix) a finding that exceeds the ceiling, with a clear log/warning attached to the finding.
3. **Run a second tier over skipped findings** — As an atcr operator, I want to point a second, more capable executor at just the findings a first pass skipped, so a two-tier cheap-then-frontier workflow works end-to-end.
4. **Validate ceiling configuration** — As an atcr maintainer, I want registry validation to reject invalid ceiling values the same way it already rejects invalid severity/timeout/tool-call values.
5. **Document the multi-tier workflow** — As an atcr operator, I want documentation and a worked example config showing how to set up a two-tier (cheap + frontier) fix run.

## Planning Success Criteria

- Executor config supports complexity ceilings (`max_estimated_minutes`, plus optional `max_severity_for_fix`), validated the same way existing executor fields are.
- `generateFixes` skips (and logs) findings whose `EstMinutes` (or severity) exceeds the executor's ceiling, without disturbing the existing confidence/severity/attribution filters.
- An executor that self-assesses a fix as too complex declines it; the decline is surfaced as a skipped finding with an explicit reason, not a silent drop or a partial fix presented as complete.
- A documented example config demonstrates a cheap-tier pass followed by a frontier-tier pass against the same `findings.json`.
- `docs/registry.md` and `docs/findings-format.md` reflect the new fields and their routing semantics.

## Risk Mitigation

1. **Risk:** Ambiguity between "ordered executor chain" and "single executor + ceiling + independently-run second pass" could cause scope creep or mid-sprint rework. **Mitigation:** Surface as a clarification before `/design-sprint` locks the phase structure — `codebase-discovery.json` already documents both options with their tradeoffs.
2. **Risk:** `EstMinutes` is a best-effort, model-emitted integer (non-numeric parses as `0`) — routing decisions built on it inherit that noise; a model can under- or over-estimate its own fix's difficulty. **Mitigation:** Treat the ceiling as a soft routing hint layered on top of the existing confidence/severity gates, not a correctness guarantee on its own.
3. **Risk:** A finding silently dropped by every configured tier (ceiling set too low everywhere) could read as a false "clean" run. **Mitigation:** Skip logging (already called for in the original epic's T4/AC3) must keep ceiling-skipped findings visible in output/logs; test coverage should assert this explicitly.

## Next Steps
1. `/find-documentation @.planning/plans/active/32.1_multi_tier_fix_execution/`
2. `/create-documentation @.planning/plans/active/32.1_multi_tier_fix_execution/`
3. `/create-user-stories @.planning/plans/active/32.1_multi_tier_fix_execution/`
4. `/create-acceptance-criteria @.planning/plans/active/32.1_multi_tier_fix_execution/`
5. `/design-sprint @.planning/plans/active/32.1_multi_tier_fix_execution/`
6. `/create-sprint @.planning/plans/active/32.1_multi_tier_fix_execution/`

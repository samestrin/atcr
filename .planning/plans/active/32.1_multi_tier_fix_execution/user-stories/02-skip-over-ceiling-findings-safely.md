# User Story 2: Skip Over-Ceiling Findings Safely

**Plan:** [32.1: Multi-Tier Fix Execution Engine](../plan.md)

## User Story

**As an** atcr operator running a cost-optimized, multi-tier fix pass
**I want** the fix engine to skip a finding whose estimated complexity exceeds my executor's configured ceiling — instead of attempting it anyway, crashing, or returning a hallucinated partial fix — and to attach a clear, non-silent reason to that finding
**So that** I can trust a cheap/local-model tier to leave hard findings untouched for a later frontier-model tier, and never mistake a silently-skipped finding for a clean run

## Story Context

- **Background:** `generateFixes` (`internal/verify/executor.go:104-232`) already runs a per-finding skip chain before dispatching a fix attempt: confidence must be at or above HIGH (`reclib.ConfidenceAtOrAbove`), severity must meet the executor's floor (`meetsSeverityFloor`, `internal/verify/severity.go:14-22`), and the finding must not already carry this executor's fix attribution (`hasFixAttribution`). This story adds a fourth condition — a complexity ceiling — as a sibling check in that same chain, plus a self-gating path for when the executor itself judges a dispatched fix too complex to complete safely. Every existing skip path in this function already follows a strict pattern: log via `logPipelineWarning` and set `f.FixWarning` on the finding, never a bare silent `continue` for the fix-consumer-visible cases (only the pre-dispatch confidence/severity/attribution skips are currently silent — this story changes that for the new ceiling case specifically, per the original epic's T4).
- **Assumptions:**
  - Story 1 (Configure a complexity ceiling) lands `ExecutorConfig.MaxEstimatedMinutes` (and optionally `MaxSeverityForFix`) with an `EffectiveXxx()` resolver, ahead of or alongside this story — the ceiling value this story routes on is Story 1's output.
  - `JSONFinding.EstMinutes` (`internal/reconcile/emit.go`) is already populated end-to-end from reviewer output; this story routes on it as-is and does not change how it is produced.
  - A ceiling of zero/unset means "no ceiling" (consistent with the existing `min_severity_for_fix` floor convention of a zero-value meaning "use default"), so existing single-tier configs are unaffected without an explicit opt-in.
- **Constraints:**
  - Must reuse existing mechanisms only: `logPipelineWarning` (`internal/verify/pipeline.go:41`) for logging, and `JSONFinding.FixWarning` (`internal/reconcile/emit.go:136`) for skip visibility in `findings.json`/report output — no new logging sink or output field.
  - Must not disturb the existing confidence/severity/attribution skip order or behavior; the ceiling check is additive, not a replacement.
  - Self-gating must never present a partial/incomplete fix as complete — a self-declined fix follows the same skip-and-log contract as a pre-dispatch ceiling skip, not a separate code path with different guarantees.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (Configure a complexity ceiling) — needs `ExecutorConfig`'s new ceiling field(s) and their `EffectiveXxx()` resolver to route on |

## Success Criteria (SMART Format)

- **Specific:** `generateFixes` skips any finding whose `EstMinutes` exceeds the executor's effective ceiling (and, if configured, whose severity exceeds `max_severity_for_fix`) before dispatching a fix attempt, logs a new `executor_ceiling_skip` warning class with the finding's `File:Line` via `logPipelineWarning`, and sets that finding's `FixWarning` to an explicit, human-readable reason — mirroring the exact pattern of the existing `executor_empty_fix`/`executor_truncated_fix` skip branches.
- **Measurable:** A table-driven test in `internal/verify/executor_test.go` (or equivalent) demonstrates: (1) a finding at/below the ceiling still gets a fix attempt, (2) a finding above the ceiling is skipped with `FixWarning` non-empty and no `Fix` set, (3) the ceiling skip does not interfere with the existing confidence/severity/attribution skip cases, and (4) a self-declined-as-too-complex fix from the executor also lands as a skip with a distinct reason, not as `Fix` content.
- **Achievable:** The change is one additional condition in an existing skip chain plus one new `logPipelineWarning` class string — no new subsystem, no schema migration beyond what Story 1 already added.
- **Relevant:** Directly implements the original epic's AC3 ("Execution Engine successfully skips findings that exceed the configured complexity boundaries"), T4 ("gracefully handle and log skipped findings when a complexity ceiling is hit"), and Proposed Solution #4 (self-gating) — the core routing behavior the whole multi-tier plan depends on.
- **Time-bound:** Implementable within a single sprint phase alongside or immediately after Story 1, since it touches one function (`generateFixes`) and one new sibling helper.

## Acceptance Criteria Overview

1. A finding whose `EstMinutes` exceeds the executor's effective `max_estimated_minutes` ceiling is skipped before any fix-generation call is made (no wasted API call), with `f.FixWarning` set to a clear reason (e.g. `"skipped: estimated complexity (Nm) exceeds executor ceiling (Mm)"`) and a `logPipelineWarning(logger, "executor_ceiling_skip", "<file>:<line>: ...")` call emitted.
2. If `max_severity_for_fix` is configured and a finding's severity exceeds it, the same skip-and-log contract applies, distinguishable in the warning detail from the estimated-minutes ceiling case.
3. When the executor itself self-assesses (during or after a dispatched attempt) that a fix is too complex to complete safely, it declines rather than returning a partial/incomplete fix — the decline is recorded via the identical `FixWarning` + `logPipelineWarning` contract, never as `Fix` content and never as a silent no-op.
4. Existing skip behavior (confidence floor, severity floor, prior-attribution idempotency guard) and existing skip/failure branches (`executor_fix_failed`, `executor_truncated_fix`, `executor_empty_fix`, `executor_invalid_syntax`) are unchanged in ordering and behavior — verified by the pre-existing executor test suite continuing to pass.
5. A ceiling-skipped or self-declined finding remains visible end-to-end in `findings.json` / report output via its non-empty `FixWarning`, so a run where every configured tier skips a finding cannot be misread as a false "clean" run.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/32.1_multi_tier_fix_execution/`_

## Technical Considerations

- **Implementation Notes:**
  - Add a ceiling-check helper alongside `meetsSeverityFloor` in `internal/verify/severity.go` (e.g. `withinComplexityCeiling(estMinutes, maxMinutes int) bool`), keeping the file's existing "one small pure predicate per rule" convention rather than inlining the comparison into `generateFixes`.
  - Insert the ceiling check into `generateFixes`'s existing pre-dispatch skip chain (`internal/verify/executor.go:104-232`), after the current confidence/severity/attribution checks, so all cheap pre-dispatch filters stay grouped together before the goroutine/semaphore dispatch.
  - Unlike the current pre-dispatch skips (which use a bare `continue` and set nothing), the new ceiling skip must call `logPipelineWarning(log.FromContext(ctx), "executor_ceiling_skip", ...)` and set `f.FixWarning` before continuing — this is the T4-mandated behavior change from "silent" to "visible."
  - Self-gating: the executor's own response (or the executor's own tool-loop reasoning in Agent Mode, per `invokeExecutor`) may indicate a decline distinct from an error/empty/truncated response. Treat a self-declined fix as its own branch parallel to the existing `warn`/`truncated`/empty-string handling inside the per-finding goroutine, with its own reason string surfaced through the same `FixWarning` field — do not overload `executor_fix_failed` (which implies a provider/transport error, not a deliberate complexity decline).
- **Integration Points:**
  - `internal/verify/severity.go` — new ceiling predicate, same file and style as `meetsSeverityFloor`.
  - `internal/verify/executor.go` (`generateFixes`) — new skip-chain condition and new self-gating branch inside the per-finding goroutine.
  - `internal/verify/pipeline.go` (`logPipelineWarning`) — reused unchanged; only a new `class` string value (`executor_ceiling_skip`) is introduced, no signature change.
  - `internal/reconcile/emit.go` (`JSONFinding.FixWarning`) — reused unchanged as the skip-visibility carrier into `findings.json`.
- **Data Requirements:** No schema change in this story — it consumes `ExecutorConfig`'s ceiling field(s) from Story 1 and `JSONFinding.EstMinutes`/`Severity`, both already present.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Ceiling skip silently reuses an existing warning class (e.g. `executor_fix_failed`) instead of its own, making ceiling-skips indistinguishable from real provider failures in logs | Medium | Require a distinct `executor_ceiling_skip` class string in code review; add a test asserting the exact class string emitted |
| `EstMinutes` is a best-effort, model-emitted integer (non-numeric parses as `0`) — a `0` value could be misread as "trivially cheap" and never skipped, or as "unknown" and always skipped, depending on ceiling semantics | Medium | Document the `0`-value convention explicitly (treat as "no estimate provided," not a real minutes value) alongside Story 1's config docs; cover with an explicit test case |
| Self-gating branch overlaps confusingly with the existing truncated/empty/failed branches inside the same goroutine, risking a finding ending up with both a stale `Fix` and a decline warning | Low | Follow the existing file's own documented invariant ("generateFixes owns FixWarning end-to-end... the valid-syntax branch clears it unconditionally") — ensure the decline branch returns before any `f.Fix` assignment, matching the early-return pattern already used by `warn`/`truncated`/empty-fix branches |

---

**Created:** July 20, 2026
**Status:** Draft - Awaiting Acceptance Criteria

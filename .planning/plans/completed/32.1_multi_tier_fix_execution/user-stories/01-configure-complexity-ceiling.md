# User Story 1: Configure a Complexity Ceiling on the Executor

**Plan:** [32.1: Multi-Tier Fix Execution Engine](../plan.md)

## User Story

**As an** atcr operator running fix generation with a cheap/local executor (e.g. Ollama/Llama 3)
**I want** to set a `max_estimated_minutes` ceiling (and an optional `max_severity_for_fix` ceiling) on that executor in `atcr.yaml`
**So that** the executor is never dispatched at findings estimated beyond its capability, protecting fix quality and avoiding wasted fix-generation calls on bugs the model is likely to botch

## Story Context

- **Background:** The executor config (`internal/registry/config.go:206-225`, `ExecutorConfig`) already gates fix eligibility with a severity *floor* — `min_severity_for_fix` (default MEDIUM) — but has no upper bound. Every finding meeting that floor with HIGH confidence is attempted regardless of estimated difficulty. AC1 of the parent epic (reviewers emitting a complexity/time estimate) is already satisfied end-to-end: `personas/_base.md` instructs every reviewer persona to emit `EST_MINUTES`, `internal/stream/parser.go` parses it into `Finding.EstMinutes`, and `internal/reconcile/emit.go` carries it through to `JSONFinding.EstMinutes` (`json:"est_minutes"`). This story adds the missing config surface — a ceiling — so a later story's routing logic (in `generateFixes`) has something to check against.
- **Assumptions:**
  - `EstMinutes` is already reliably present on `JSONFinding` by the time fix generation runs; no new reviewer/parser/emit work is needed for this story.
  - `EstMinutes` is a best-effort, model-emitted integer (non-numeric parses as `0`); the ceiling is a soft routing hint layered on existing confidence/severity gates, not a hard correctness guarantee.
  - A ceiling of `0` or an unset field means "no ceiling" (unlimited), preserving current no-ceiling behavior as the default — backward compatible with every existing `atcr.yaml`.
- **Constraints:**
  - Must follow the existing `ExecutorConfig` field convention exactly: a `yaml:"...,omitempty"` tag, a doc comment explaining the field's role, a named-constant + explicit-range-check in `validateExecutor` (`internal/registry/config.go:593-677`), and its own `EffectiveXxx()` resolver method (precedent: `EffectiveFixMinSeverity`, `EffectiveMaxToolCalls`).
  - This story is config-only — it does NOT implement the skip/routing logic in `generateFixes` or the skip-visibility logging (those are later stories in this plan). The ceiling fields must exist, validate, and resolve correctly, but nothing consumes them yet.
  - Must not introduce a parallel `complexity_score` concept — route strictly on the existing `EstMinutes` field.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `ExecutorConfig` gains a `MaxEstimatedMinutes *int` field (yaml: `max_estimated_minutes`) and a `MaxSeverityForFix string` field (yaml: `max_severity_for_fix`), each validated in `validateExecutor` and each exposed via a dedicated `EffectiveMaxEstimatedMinutes()` / `EffectiveMaxSeverityForFix()` resolver method.
- **Measurable:** New unit tests in `internal/registry` cover: valid ceiling values load and validate cleanly; a negative or absurdly large `max_estimated_minutes` is rejected with a clear error; a `max_severity_for_fix` outside the canonical severity set is rejected; an unset ceiling resolves to "no ceiling" via the effective-value resolver; a `max_severity_for_fix` set below `min_severity_for_fix` is rejected as a contradictory range.
- **Achievable:** Mirrors an established pattern already used four times in the same file (`MinSeverity`, `TimeoutSecs`, `Temperature`, `MaxToolCalls`) — no new architecture required.
- **Relevant:** Without a config surface for the ceiling, no routing/skip logic anywhere in this plan has anything to read; this story unblocks every subsequent story in the plan.
- **Time-bound:** Small, self-contained change to one file (plus its test file) — completable within a single TDD cycle.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-executorconfig-exposes-complexity-ceiling-fields.md) | ExecutorConfig Exposes Complexity Ceiling Fields | Unit |
| [01-02](../acceptance-criteria/01-02-effective-value-resolvers-return-correct-defaults.md) | Effective-Value Resolvers Return Correct Defaults | Unit |

## Original Criteria Overview

1. `ExecutorConfig` exposes `max_estimated_minutes` (int, omitempty) and `max_severity_for_fix` (string, omitempty) as new optional YAML fields, both backward-compatible (absent = no ceiling).
2. `validateExecutor` rejects an invalid `max_estimated_minutes` (non-positive when set, or above a defined max constant) and an invalid `max_severity_for_fix` (not one of CRITICAL/HIGH/MEDIUM/LOW, or set below `min_severity_for_fix` such that the effective range is empty), accumulating errors like every other executor field check.
3. `EffectiveMaxEstimatedMinutes()` and `EffectiveMaxSeverityForFix()` resolver methods return the configured ceiling when set, and an explicit "no ceiling" sentinel/zero-value when unset — ready for a later story's `generateFixes` routing logic to consume without re-deriving the fallback rule.

## Technical Considerations

- **Implementation Notes:**
  - Add `MaxEstimatedMinutes *int` (yaml: `max_estimated_minutes,omitempty`) — a pointer so an explicit `0` (if ever meaningful) is distinguishable from unset, matching the `TimeoutSecs`/`MaxToolCalls` pointer convention.
  - Add `MaxSeverityForFix string` (yaml: `max_severity_for_fix,omitempty`) — plain string like `MinSeverity`, normalized via the same `reclib.NormalizeSeverity` call used for `MinSeverity` in `validateExecutor`.
  - Introduce a new named constant (e.g. `MaxExecutorEstimatedMinutes`) alongside the existing `MaxExecutorToolCalls`/`MaxExecutorRules` constants for the upper bound on `max_estimated_minutes`.
  - Cross-field check: when both `MinSeverity`/`MaxSeverityForFix` are set, validate that the severity floor does not exceed the severity ceiling (an operator typo that would make the executor permanently ineligible).
- **Integration Points:** `internal/registry/config.go` (`ExecutorConfig` struct, `validateExecutor`); no other package needs to change for this story — `internal/verify/executor.go`'s `generateFixes` consumption of these fields is explicitly out of scope here (later story).
- **Data Requirements:** None beyond the two new optional YAML fields; no migration needed since both are `omitempty` and absent-safe.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Operator sets `max_severity_for_fix` below `min_severity_for_fix`, creating a contradictory (always-empty) eligibility range | Medium | Add an explicit cross-field validation error in `validateExecutor` so the misconfiguration is caught at load time, not silently discovered as "the executor never fixes anything" |
| New field added but not yet consumed (this story is config-only) could look "broken" in isolation if reviewed without plan context | Low | Document in the story and PR description that routing/skip logic lands in a subsequent story in this same plan; validation/resolver tests prove the surface works correctly on its own |
| Divergence from the established field-convention style (constant naming, comment tone, resolver signature) makes the codebase inconsistent | Low | Explicitly mirror `EffectiveFixMinSeverity`/`EffectiveMaxToolCalls` naming and doc-comment structure; review diff against those two methods before finalizing |

---

**Created:** July 20, 2026
**Status:** Draft - Acceptance Criteria Defined (refined July 20, 2026)

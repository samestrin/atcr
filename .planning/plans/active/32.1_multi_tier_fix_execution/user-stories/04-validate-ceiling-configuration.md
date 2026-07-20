# User Story 4: Validate Ceiling Configuration

**Plan:** [32.1: Multi-Tier Fix Execution Engine](../plan.md)

## User Story

**As an** atcr maintainer responsible for `internal/registry/config.go`
**I want** `validateExecutor` to reject invalid `max_estimated_minutes` and `max_severity_for_fix` values with the same named-constant + explicit-range-check rigor it already applies to `min_severity_for_fix`, `timeout_secs`, `temperature`, and `max_tool_calls`
**So that** a maintainer or operator misconfiguration of a new ceiling field fails loudly at load time instead of silently producing wrong routing behavior at fix-generation time

## Story Context

- **Background:** Story 1 introduces the `MaxEstimatedMinutes *int` and `MaxSeverityForFix string` fields on `ExecutorConfig` so the config surface exists. This story is the dedicated correctness/hardening pass on that surface: it ensures both new fields follow the exact validation convention every other `validateExecutor` field already follows — a named constant near the top of `config.go` (alongside `MaxExecutorPersonaLen`, `MaxTimeoutSecs`, `MaxExecutorSystemPromptLen`, `MaxExecutorRules`, `MaxExecutorRuleLen`, `MaxExecutorToolCalls`), an explicit range check that accumulates into the shared `errs` slice (never short-circuits, per the Epic 4.2/AC6 convention already documented on `validateExecutor`), and a cross-field check that `max_severity_for_fix` cannot be set below the existing `min_severity_for_fix` floor. `validateExecutor` (`internal/registry/config.go:593-677`) is the single, unconditional validation gate for every executor field — there is no other place these fields get checked, so gaps here are gaps in the entire system's correctness guarantee.
- **Assumptions:**
  - Story 1 has already added the `MaxEstimatedMinutes` and `MaxSeverityForFix` fields to the `ExecutorConfig` struct with at least a first-pass validation; this story's job is to confirm/harden that validation against the established convention and lock it down with the same depth of test coverage every existing ceiling-style field has (see `TestExecutor_MaxToolCallsOutOfRangeRejected`, `internal/registry/executor_config_test.go:543`).
  - Both new fields are optional (nil/empty = no ceiling); validation only fires when a value is explicitly set, mirroring `TimeoutSecs`/`MaxToolCalls`.
  - `max_severity_for_fix` reuses `reclib.NormalizeSeverity` and the existing `reviewSeverities` set — no new severity vocabulary is introduced.
- **Constraints:**
  - Must not change the *meaning* of the fields (that's Story 1's scope) — this story is strictly about validation completeness and consistency, plus the cross-field floor/ceiling contradiction check.
  - Must not touch `generateFixes` or any routing/skip logic (Stories 2-3) — this story never reads these fields outside of `validateExecutor`.
  - New named constants must live alongside the existing `MaxExecutor*` constants block, following the same doc-comment style (purpose + rationale, matching `MaxExecutorToolCalls`'s comment referencing `MaxAgentTurns`).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Story 1 (Configure a Complexity Ceiling) — the fields under validation must exist on `ExecutorConfig` first |

## Success Criteria (SMART Format)

- **Specific:** `validateExecutor` rejects a non-positive or over-cap `max_estimated_minutes` against a new named constant (e.g. `MaxExecutorEstimatedMinutes`), rejects a `max_severity_for_fix` outside the canonical CRITICAL/HIGH/MEDIUM/LOW set (mirroring the existing `min_severity_for_fix` check), and rejects a `max_severity_for_fix` set below `min_severity_for_fix` as a contradictory (always-empty) eligibility range.
- **Measurable:** At least three new tests land in `internal/registry/executor_config_test.go` following the existing naming convention: `TestExecutor_MaxEstimatedMinutesOutOfRangeRejected` (direct analog of `TestExecutor_MaxToolCallsOutOfRangeRejected`, line 543), `TestExecutor_MaxSeverityForFixInvalidRejected` (analog of `TestExecutor_InvalidMinSeverityForFix`, line 112), and `TestExecutor_MaxSeverityForFixBelowMinSeverityRejected` (the new cross-field contradiction case) — plus a positive-path test confirming a valid ceiling combination loads and validates cleanly.
- **Achievable:** Every check is a direct structural copy of an existing pattern in the same function (`validateExecutor`); no new validation architecture, just two more field checks plus one cross-field comparison using the already-imported `reviewSeverities` ordering.
- **Relevant:** Bad ceiling config today would fail silently — the executor would simply never fire (or always fire) with no diagnostic — which is exactly the class of bug the existing `validateExecutor` convention exists to prevent for every other field.
- **Time-bound:** Small, single-file (plus test file) change completable within one TDD cycle once Story 1's fields exist.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-numeric-and-severity-ceiling-values-are-range-validated.md) | Numeric and Severity Ceiling Values Are Range-Validated | Unit |
| [04-02](../acceptance-criteria/04-02-floor-ceiling-contradiction-is-rejected-at-load-time.md) | Floor-Ceiling Contradiction Is Rejected at Load Time | Unit |

## Original Criteria Overview

1. `validateExecutor` rejects a `max_estimated_minutes` that is non-positive or exceeds a new named constant (e.g. `MaxExecutorEstimatedMinutes`), accumulating the error alongside all other executor faults rather than short-circuiting.
2. `validateExecutor` rejects a `max_severity_for_fix` that does not normalize to one of CRITICAL/HIGH/MEDIUM/LOW via `reclib.NormalizeSeverity`, using the same error-message style as the existing `min_severity_for_fix` check.
3. `validateExecutor` rejects a configuration where `max_severity_for_fix` (ceiling) is set below `min_severity_for_fix` (floor), since that combination makes the executor permanently ineligible for any finding — with a test asserting the specific contradictory-range error.

## Technical Considerations

- **Implementation Notes:**
  - Add a new named constant (e.g. `MaxExecutorEstimatedMinutes`) in the constants block near `MaxExecutorToolCalls`/`MaxExecutorRules`, with a doc comment explaining the bound choice (e.g. capping at a large-but-finite value to reject obvious typos like an extra zero, not to impose a "realistic" fix-time opinion).
  - The `max_estimated_minutes` range check follows the `TimeoutSecs` shape exactly: `if e.MaxEstimatedMinutes != nil && (*e.MaxEstimatedMinutes <= 0 || *e.MaxEstimatedMinutes > MaxExecutorEstimatedMinutes) { errs = append(...) }`.
  - The `max_severity_for_fix` normalization check follows the existing `MinSeverity` check exactly: `if normalized := reclib.NormalizeSeverity(e.MaxSeverityForFix); normalized != "" && !reviewSeverities[normalized] { errs = append(...) }`.
  - The floor/ceiling contradiction check needs a severity ordering comparison (CRITICAL > HIGH > MEDIUM > LOW); reuse whatever ordering helper `reclib` or `internal/verify/severity.go` already exposes rather than inventing a new rank table — if none exists, this is the smallest possible local rank map scoped to this one comparison.
  - Every new error message must match the existing tone: `"executor: <field> must be ..."`, so error output stays consistent for operators scanning `atcr.yaml` validation failures.
- **Integration Points:** `internal/registry/config.go` (constants block + `validateExecutor`) and `internal/registry/executor_config_test.go` (new tests) only. No other package changes.
- **Data Requirements:** None — validation-only change against fields Story 1 already defines; no schema migration.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Cross-field floor/ceiling check needs a severity ranking that doesn't yet exist as a reusable helper, tempting an ad-hoc string-list hack | Medium | Scope the rank comparison narrowly to this one check; if no shared ordering helper exists, add the smallest local rank map and note it as a candidate for later consolidation rather than blocking this story |
| New named constant's bound (e.g. `MaxExecutorEstimatedMinutes`) is picked arbitrarily and later needs revision as real usage patterns emerge | Low | Document the rationale in the constant's doc comment (typo-guard, not a policy opinion) so future changes are a one-line constant edit, not a design debate |
| Test names/structure drift from the established `TestExecutor_*` convention, making the suite harder to navigate | Low | Explicitly mirror `TestExecutor_MaxToolCallsOutOfRangeRejected` and `TestExecutor_InvalidMinSeverityForFix` naming and structure before finalizing |

---

**Created:** July 20, 2026
**Status:** Draft - Acceptance Criteria Defined (refined July 20, 2026)

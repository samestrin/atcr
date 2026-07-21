# Acceptance Criteria: Ceiling-Exceeding Findings Are Skipped Before Dispatch

**Related User Story:** [02: Skip Over-Ceiling Findings Safely](../user-stories/02-skip-over-ceiling-findings-safely.md)

## Acceptance Criteria
A finding whose `EstMinutes` (or severity) exceeds the executor's configured ceiling is skipped before any provider call — no wasted API call — with a non-empty `FixWarning` and exactly one `executor_ceiling_skip` pipeline warning carrying the finding's `File:Line`. Findings at/below the ceiling, and all findings under an unset (zero) ceiling, are dispatched exactly as today.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function / `internal/verify` package | Pure predicate + pre-dispatch skip-chain condition |
| Test Framework | go test | Table-driven tests in `internal/verify/executor_test.go` |
| Key Dependencies | `internal/registry` (`ExecutorConfig`), `internal/reconcile` (`JSONFinding`), `internal/log` (`logPipelineWarning`) | No new packages |

### Related Files (from codebase-discovery.json)
- `internal/verify/severity.go` - modify: add `withinComplexityCeiling(estMinutes, maxMinutes int) bool` (and, if Story 1 exposes a severity ceiling, an analogous severity-ceiling predicate), following the existing "one small pure predicate per rule" convention used by `meetsSeverityFloor`.
- `internal/verify/executor.go` - modify: insert the ceiling check into `generateFixes`'s pre-dispatch skip chain (around lines 136-149), after the existing confidence/severity/attribution checks and before `wg.Add(1)`/goroutine dispatch; on skip, call `logPipelineWarning(log.FromContext(ctx), "executor_ceiling_skip", "<file>:<line>: ...")` and set `f.FixWarning`.
- `internal/verify/executor_test.go` - modify: add table-driven cases for at/below-ceiling (fix attempted) and above-ceiling (skipped, warned) findings.
- `internal/verify/severity_test.go` - modify: add unit cases for the new ceiling predicate (inclusive boundary, zero-estimate, above-ceiling, and defensive negative values) alongside the existing `meetsSeverityFloor` cases.
- `internal/verify/pipeline.go` - reference only: `logPipelineWarning` (line 41) is reused unchanged; only the new `"executor_ceiling_skip"` class string is introduced.

## Happy Path Scenarios
**Scenario 1: Finding at or below the ceiling proceeds to fix generation**
- **Given** an `ExecutorConfig` with an effective `MaxEstimatedMinutes` of 30, and a finding with `EstMinutes` of 30 that already passes the confidence/severity/attribution checks
- **When** `generateFixes` runs
- **Then** the finding is dispatched to a fix attempt (its snippet is read and the executor is called) exactly as it would be with no ceiling configured, and no `executor_ceiling_skip` warning is emitted for it

**Scenario 2: Finding above the ceiling is skipped before any fix-generation call**
- **Given** the same `ExecutorConfig` (ceiling 30) and a finding with `EstMinutes` of 120 that would otherwise pass confidence/severity/attribution
- **When** `generateFixes` runs
- **Then** no snippet read or executor call is made for that finding, `f.FixWarning` is set to a clear, human-readable reason (e.g. `"skipped: estimated complexity (120m) exceeds executor ceiling (30m)"`), `f.Fix` remains unset/unchanged, and `logPipelineWarning(logger, "executor_ceiling_skip", "<file>:<line>: ...")` is called exactly once for that finding

**Scenario 3: No ceiling configured (zero/unset) never skips on this basis**
- **Given** an `ExecutorConfig` whose `MaxEstimatedMinutes` resolves to 0 (unset, per Story 1's "no ceiling" convention)
- **When** `generateFixes` runs over findings with a range of `EstMinutes` values, including very large ones
- **Then** none of them are skipped via the ceiling path, preserving existing single-tier config behavior with no explicit opt-in

## Edge Cases
**Edge Case 1: EstMinutes of exactly the ceiling value**
- **Given** a finding with `EstMinutes` equal to the ceiling (boundary, e.g. both 30)
- **When** evaluated by `withinComplexityCeiling`
- **Then** the finding is treated as within the ceiling (inclusive boundary — "at or below" per the story's Success Criteria) and is not skipped

**Edge Case 2: EstMinutes of zero (no estimate provided)**
- **Given** a finding whose `EstMinutes` is 0 (model did not emit a numeric estimate, per emit.go's best-effort parsing)
- **When** the ceiling check runs with a nonzero configured ceiling
- **Then** zero is treated as "no estimate provided" (not a real minutes value) and does NOT trigger a ceiling skip on that basis alone, per the story's documented `0`-value convention; this exact case is covered by an explicit test

**Edge Case 3: Ceiling skip alongside a configured severity ceiling**
- **Given** both `max_estimated_minutes` and `max_severity_for_fix` configured, and a finding that exceeds only the severity ceiling (not the minutes ceiling)
- **When** `generateFixes` runs
- **Then** the finding is skipped via the severity-ceiling branch with a warning detail distinguishable from the estimated-minutes case (different reason text), while a finding exceeding only the minutes ceiling produces the minutes-specific reason text

**Edge Case 4: Multiple ceiling-exceeding findings in one run**
- **Given** several findings in the same `generateFixes` call that each exceed the ceiling
- **Then** each gets its own independent `f.FixWarning` and its own `logPipelineWarning` call carrying that finding's own `File:Line`, with no cross-finding leakage of state (mirrors the per-index-write invariant already documented for the goroutine pool)

## Error Conditions
**Error Scenario 1: Ceiling check must not consume/require a provider call**
- Since the ceiling skip happens in the cheap pre-dispatch filter stage (mirroring confidence/severity/attribution checks), there is no error path here — the check is a pure comparison. Any test asserting this AC must also assert (via a spy/counting completer) that `complete.Complete`/`callExecutor` is never invoked for a ceiling-skipped finding, proving "no wasted API call."
- Error message (for FixWarning): a clear, non-empty string such as `"skipped: estimated complexity (Nm) exceeds executor ceiling (Mm)"` — never empty, never a stack trace or raw error type.

## Performance Requirements
- **Response Time:** The ceiling check is an O(1) integer comparison; it must add no measurable latency to `generateFixes`'s per-finding pre-dispatch filtering (same cost class as the existing confidence/severity checks).
- **Throughput:** Skipping ceiling-exceeding findings before dispatch must reduce, not increase, total executor round-trips for a given run — the explicit cost-control goal of this AC (no wasted API call).

## Security Considerations
- **Authentication/Authorization:** Not applicable — this is a local, in-process comparison against config-derived values, no new external call or credential surface.
- **Input Validation:** `EstMinutes` and the ceiling are both plain `int` values already validated/defaulted upstream (Story 1's config loading, emit.go's parsing); the predicate must not panic on zero, negative, or arbitrarily large values — treat negative `EstMinutes` (should not occur, but defensively) as "no valid estimate" rather than crashing or under/over-skipping.

## Test Implementation Guidance
**Test Type:** UNIT (table-driven, `internal/verify/severity_test.go` for the pure predicate; `internal/verify/executor_test.go` for the `generateFixes` skip-chain integration using a scripted/spy `executorCompleter`)
**Test Data Requirements:** `reconcile.JSONFinding` values spanning `EstMinutes` at 0, below-ceiling, exactly-at-ceiling, and above-ceiling; `registry.ExecutorConfig` values with ceiling unset (0), a typical nonzero ceiling, and (for Edge Case 3) both minutes and severity ceilings configured together.
**Mock/Stub Requirements:** A spy `executorCompleter` that records whether `Complete`/`CompleteWithMeta` was invoked, to prove ceiling-skipped findings never reach the provider call; a captured/observable logger (or a test hook) to assert the `"executor_ceiling_skip"` class string and its `File:Line`-bearing detail.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `withinComplexityCeiling` (or equivalently named predicate) exists in `internal/verify/severity.go` and is unit-tested at the ceiling boundary, zero, and above-ceiling values
- [x] A ceiling-exceeding finding never reaches `callExecutor`/`invokeExecutor` (spy-verified), has `f.FixWarning` set to a non-empty reason, and triggers exactly one `logPipelineWarning(..., "executor_ceiling_skip", ...)` call with `File:Line` in the detail
- [x] A zero/unset ceiling never triggers a ceiling-based skip (regression-proofing the "no explicit opt-in" contract)
- [x] Minutes-ceiling and severity-ceiling skip reasons are distinguishable in `FixWarning` text

**Manual Review:**
- [x] Code reviewed and approved

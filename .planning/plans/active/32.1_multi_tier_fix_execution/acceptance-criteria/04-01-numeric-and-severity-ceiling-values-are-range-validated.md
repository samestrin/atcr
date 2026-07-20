# Acceptance Criteria: Numeric and Severity Ceiling Values Are Range-Validated

**Related User Story:** [04: Validate Ceiling Configuration](../user-stories/04-validate-ceiling-configuration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go validation function / `internal/registry` package | Extends `validateExecutor` (`internal/registry/config.go:593-677`) — the single, unconditional gate for every `executor:` field |
| Test Framework | go test (`testify/assert`, `testify/require`) | Mirrors the table-driven style of `TestExecutor_MaxToolCallsOutOfRangeRejected` |
| Key Dependencies | `github.com/samestrin/atcr/reconcile` (`reclib.NormalizeSeverity`), existing `reviewSeverities` map (`internal/registry/config.go:375`) | No new severity vocabulary introduced |

## Related Files
- `internal/registry/config.go` - modify: add `MaxExecutorEstimatedMinutes` named constant to the `MaxExecutor*` constants block (near `MaxExecutorToolCalls`/`MaxExecutorRules`, `internal/registry/config.go:72-`), add a `MaxEstimatedMinutes *int` range check and a `MaxSeverityForFix` normalization check inside `validateExecutor` (`internal/registry/config.go:593-677`), following the existing `TimeoutSecs` (line 633) and `MinSeverity` (line 610) check shapes exactly.
- `internal/registry/executor_config_test.go` - modify: add `TestExecutor_MaxEstimatedMinutesOutOfRangeRejected` (direct analog of `TestExecutor_MaxToolCallsOutOfRangeRejected`, line 543), `TestExecutor_MaxEstimatedMinutesBoundaryAccepted` (analog of `TestExecutor_MaxToolCallsBoundaryAccepted`, line 557), and `TestExecutor_MaxSeverityForFixInvalidRejected` (analog of `TestExecutor_InvalidMinSeverityForFix`, line 112).
- `internal/registry/registry.go` (or wherever `ExecutorConfig` is declared, per Story 1) - reference only: confirms the `MaxEstimatedMinutes *int` and `MaxSeverityForFix string` fields exist before this story's checks compile against them.

**Minimum 2 files per AC**

## Happy Path Scenarios
**Scenario 1: A valid max_estimated_minutes loads cleanly**
- **Given** an `executor:` block with `provider`, `model`, and `max_estimated_minutes: 45` set
- **When** `LoadRegistry` parses and validates the config
- **Then** `LoadRegistry` returns no error and `reg.Executor.MaxEstimatedMinutes` is a non-nil pointer equal to 45

**Scenario 2: A valid max_severity_for_fix normalizes like min_severity_for_fix**
- **Given** an `executor:` block with `max_severity_for_fix: high` (lowercase)
- **When** `LoadRegistry` parses and validates the config
- **Then** `LoadRegistry` returns no error and `reg.Executor.MaxSeverityForFix` is normalized to `"HIGH"`, mirroring `TestExecutor_MinSeverityForFixExplicitAndNormalized` (`internal/registry/executor_config_test.go:61`)

**Scenario 3: An unset max_estimated_minutes and max_severity_for_fix are valid (optional fields)**
- **Given** an `executor:` block with only `provider` and `model` set
- **When** `LoadRegistry` parses and validates the config
- **Then** `LoadRegistry` returns no error, `reg.Executor.MaxEstimatedMinutes` is nil, and `reg.Executor.MaxSeverityForFix` is empty — no ceiling means no restriction, matching the `TimeoutSecs`/`MaxToolCalls` unset convention

## Edge Cases
**Edge Case 1: max_estimated_minutes at the exact boundary values (1 and MaxExecutorEstimatedMinutes) is accepted**
- **Given** `max_estimated_minutes` set to `1` and separately to `MaxExecutorEstimatedMinutes`
- **When** `LoadRegistry` validates each config
- **Then** both load without error, mirroring `TestExecutor_MaxToolCallsBoundaryAccepted` (`internal/registry/executor_config_test.go:557`)

**Edge Case 2: max_severity_for_fix is case-insensitive and whitespace-trimmed**
- **Given** `max_severity_for_fix: " Critical "` (mixed case, surrounding whitespace)
- **When** `LoadRegistry` validates the config
- **Then** it normalizes to `"CRITICAL"` via `reclib.NormalizeSeverity` with no error, matching the existing `min_severity_for_fix` normalization behavior

**Edge Case 3: Zero and negative max_estimated_minutes are both rejected, not just negative**
- **Given** `max_estimated_minutes: 0`
- **When** `LoadRegistry` validates the config
- **Then** `LoadRegistry` returns an error containing `max_estimated_minutes` — zero is non-positive and must fail the same as a negative value, per the `TimeoutSecs` convention (`*e.TimeoutSecs <= 0`)

## Error Conditions
**Error Scenario 1: max_estimated_minutes non-positive or over-cap is rejected**
- Error message: `"executor: max_estimated_minutes must be within 1..<MaxExecutorEstimatedMinutes>"` (exact wording mirrors `"executor: fix_timeout must be within 1..%d"`, `internal/registry/config.go:634`)
- HTTP status / error code: N/A (config-load-time error via `errors.Join`, not an HTTP path); test asserts via `assert.Contains(t, err.Error(), "max_estimated_minutes")`

**Error Scenario 2: max_severity_for_fix outside the canonical severity set is rejected**
- Error message: `"executor: max_severity_for_fix must be one of CRITICAL, HIGH, MEDIUM, LOW, got %q"` (exact wording mirrors the existing `min_severity_for_fix` message, `internal/registry/config.go:611`)
- HTTP status / error code: N/A (config-load-time error); test asserts via `assert.Contains(t, err.Error(), "max_severity_for_fix")`

**Error Scenario 3: Both faults accumulate rather than short-circuit**
- Error message: a single `LoadRegistry` call with both `max_estimated_minutes: -1` and `max_severity_for_fix: BLOCKER` set must produce one joined error containing BOTH `"max_estimated_minutes"` and `"max_severity_for_fix"` substrings (per the Epic 4.2/AC6 accumulate-don't-short-circuit convention already documented on `validateExecutor`)
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** Validation is a pure in-memory range/set comparison on already-parsed config fields — must add no measurable overhead to `LoadRegistry` (sub-millisecond, consistent with the existing `TimeoutSecs`/`MaxToolCalls` checks it mirrors).
- **Throughput:** N/A — `validateExecutor` runs once per `LoadRegistry` call at process/config-reload time, not on a hot request path.

## Security Considerations
- **Authentication/Authorization:** N/A — this is a local config-file validation gate, not a network-facing surface.
- **Input Validation:** `max_estimated_minutes` must reject non-positive and over-cap integer values via the new `MaxExecutorEstimatedMinutes` constant (a typo-guard ceiling, not a policy opinion, per the story's implementation notes) so a misconfigured YAML value (e.g. an extra zero) fails loudly at load rather than silently producing wrong routing behavior later. `max_severity_for_fix` must reject any value that does not normalize to one of the four canonical severities, preventing an unrecognized string from silently disabling the ceiling (a permissive fallback) or matching nothing at fix-generation time.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Minimal `executorBaseProviders`-based YAML fixtures (`internal/registry/executor_config_test.go:13`) with `provider`/`model` plus the field under test; table-driven cases for out-of-range values (`"0"`, `"-1"`, `fmt.Sprintf("%d", MaxExecutorEstimatedMinutes+1)`) mirroring `TestExecutor_MaxToolCallsOutOfRangeRejected` (line 543-554).
**Mock/Stub Requirements:** None — `LoadRegistry` reads from a real temp file written via the existing `writeRegistry(t, ...)` test helper; no external services or mocks needed.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `MaxExecutorEstimatedMinutes` constant added to the `MaxExecutor*` block with a doc comment explaining the bound as a typo-guard (not a policy opinion), matching `MaxExecutorToolCalls`'s comment style
- [ ] `validateExecutor` rejects non-positive and over-cap `max_estimated_minutes`, accumulating into `errs` without short-circuiting
- [ ] `validateExecutor` rejects a `max_severity_for_fix` that does not normalize to CRITICAL/HIGH/MEDIUM/LOW via `reclib.NormalizeSeverity`
- [ ] New tests `TestExecutor_MaxEstimatedMinutesOutOfRangeRejected`, `TestExecutor_MaxEstimatedMinutesBoundaryAccepted`, and `TestExecutor_MaxSeverityForFixInvalidRejected` pass and follow the existing `TestExecutor_*` naming convention

**Manual Review:**
- [ ] Code reviewed and approved

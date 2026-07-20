# Acceptance Criteria: Floor-Ceiling Contradiction Is Rejected at Load Time

**Related User Story:** [04: Validate Ceiling Configuration](../user-stories/04-validate-ceiling-configuration.md)

## Acceptance Criteria
When both severity bounds are set and each individually normalizes to a canonical severity, `validateExecutor` rejects a `max_severity_for_fix` ranking strictly below `min_severity_for_fix` with a distinguishable contradictory-range error naming both configured values — using the shared `reconcile.SeverityRank` map (no new rank table) — a load-time guarantee that never fires at fix-generation time.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go validation function / `internal/registry` package | Cross-field check inside `validateExecutor` (`internal/registry/config.go:593-677`), runs after both `MinSeverity` (line 610) and `MaxSeverityForFix` are individually normalized/validated |
| Test Framework | go test (`testify/assert`, `testify/require`) | Mirrors `TestExecutor_InvalidMinSeverityForFix` (`internal/registry/executor_config_test.go:112`) assertion style |
| Key Dependencies | `github.com/samestrin/atcr/reconcile.SeverityRank` (`reconcile/severity.go:19-24`: `{"CRITICAL":4,"HIGH":3,"MEDIUM":2,"LOW":1}`) — an existing canonical ranked map, reused rather than a new local rank table | `reclib.NormalizeSeverity` for both fields before ranking |

### Related Files (from codebase-discovery.json)
- `internal/registry/config.go` - modify: add a cross-field check inside `validateExecutor` (`internal/registry/config.go:593-677`) comparing `reclib.SeverityRank[normalizedMaxSeverity]` against `reclib.SeverityRank[normalizedMinSeverity]` — only when BOTH fields are explicitly set and each individually normalizes to a known severity — and append an error when the ceiling ranks below the floor.
- `internal/registry/executor_config_test.go` - modify: add `TestExecutor_MaxSeverityForFixBelowMinSeverityRejected` (the new cross-field contradiction case named in the story's Measurable criterion) plus a positive-path test confirming a valid floor/ceiling combination (e.g. `min_severity_for_fix: LOW`, `max_severity_for_fix: HIGH`) loads cleanly.
- `reconcile/severity.go` - reference only: confirms `SeverityRank` (lines 19-24) is the correct, already-exported ranking source to reuse — this story must NOT introduce a second, uncoordinated rank map (the severity-ordering pattern is already duplicated in other shapes — a rank map in `internal/debt/debt.go:138`, an ordered slice in `internal/debt/aggregate.go:84` — which this story explicitly avoids repeating).

**Minimum 2 files per AC**

## Happy Path Scenarios
**Scenario 1: A valid floor/ceiling combination (ceiling at or above floor) loads cleanly**
- **Given** an `executor:` block with `min_severity_for_fix: LOW` and `max_severity_for_fix: HIGH`
- **When** `LoadRegistry` parses and validates the config
- **Then** `LoadRegistry` returns no error, and both `reg.Executor.MinSeverity` (`"LOW"`) and `reg.Executor.MaxSeverityForFix` (`"HIGH"`) are set as configured

**Scenario 2: An equal floor and ceiling is a valid (single-severity) eligibility range**
- **Given** an `executor:` block with `min_severity_for_fix: HIGH` and `max_severity_for_fix: HIGH`
- **When** `LoadRegistry` parses and validates the config
- **Then** `LoadRegistry` returns no error — an equal floor/ceiling is a non-empty, single-value range (only HIGH findings are eligible), distinct from the contradictory case where the ceiling ranks strictly below the floor

## Edge Cases
**Edge Case 1: Only one of the two fields is set**
- **Given** an `executor:` block with `max_severity_for_fix: MEDIUM` set but `min_severity_for_fix` left unset (defaults to `DefaultFixMinSeverity` = `"MEDIUM"` per `applyDefaults`)
- **When** `LoadRegistry` validates the config
- **Then** the cross-field check compares the ceiling against the effective/defaulted floor (MEDIUM vs MEDIUM) — not skipped merely because the floor was not explicitly written in YAML — and this case (equal) loads cleanly per Scenario 2's rule

**Edge Case 2: Both fields individually invalid — cross-field check does not mask the per-field errors**
- **Given** an `executor:` block with `min_severity_for_fix: BOGUS` and `max_severity_for_fix: ALSO_BOGUS`
- **When** `LoadRegistry` validates the config
- **Then** `LoadRegistry` returns an error containing both the `min_severity_for_fix` and `max_severity_for_fix` individual-invalidity messages; the cross-field rank comparison must not fire (and must not panic on an unranked key) when either side fails to normalize to a canonical severity

**Edge Case 3: Contradiction detected regardless of case/whitespace input**
- **Given** an `executor:` block with `min_severity_for_fix: " Critical "` and `max_severity_for_fix: "low"`
- **When** `LoadRegistry` validates the config
- **Then** both values normalize before ranking (CRITICAL floor vs LOW ceiling), and `LoadRegistry` returns the contradictory-range error — normalization is not bypassable via case or whitespace tricks

## Error Conditions
**Error Scenario 1: max_severity_for_fix ranked below min_severity_for_fix is rejected**
- Error message: `"executor: max_severity_for_fix (LOW) must not be below min_severity_for_fix (CRITICAL) — this combination is never eligible for a fix"` (exact wording is an implementation choice, but MUST name both configured values and must be distinguishable in tone/content from the plain out-of-set message in AC 04-01, per the story's Success Criteria: "a test asserting the specific contradictory-range error")
- HTTP status / error code: N/A (config-load-time error via `errors.Join`); test asserts via `assert.Contains(t, err.Error(), "max_severity_for_fix")` AND a distinguishing substring (e.g. `"below"` or `"never eligible"`) not present in the AC 04-01 out-of-set error, so the two failure modes are not confusable by a maintainer reading `LoadRegistry`'s combined error output

## Performance Requirements
- **Response Time:** The rank comparison is two map lookups plus an integer comparison on already-normalized strings — sub-millisecond, no additional overhead to `LoadRegistry`.
- **Throughput:** N/A — runs once per `LoadRegistry` call, not on a hot request/fix-generation path (Story 2/3's `generateFixes` never re-checks this; it is a load-time-only guarantee).

## Security Considerations
- **Authentication/Authorization:** N/A — local config-file validation gate, not network-facing.
- **Input Validation:** The contradiction check must run only after both fields are confirmed to normalize to a canonical severity (reusing the AC 04-01 checks), so it never indexes `reclib.SeverityRank` with an unranked/empty key (which would either panic on a nil map access pattern or silently compare against the zero value, producing a false-positive or false-negative contradiction). This protects the executor's eligibility gate from silently defaulting to "always eligible" or "never eligible" on a malformed input that the earlier checks should have already caught.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `executorBaseProviders`-based YAML fixtures (`internal/registry/executor_config_test.go:13`) combining `min_severity_for_fix` and `max_severity_for_fix` in contradictory, equal, and valid-range combinations; one case relying on the defaulted (unset) floor to confirm the check reads the effective floor, not just the literal YAML.
**Mock/Stub Requirements:** None — `LoadRegistry` reads from a real temp file via `writeRegistry(t, ...)`; no mocks needed.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Cross-field check added to `validateExecutor` comparing `reclib.SeverityRank[maxSeverityForFix]` against `reclib.SeverityRank[minSeverityForFix]` (using the existing shared `reconcile.SeverityRank` map, not a new local rank table)
- [ ] Check fires only when both fields normalize successfully (no panic/false-positive on an already-invalid individual field)
- [ ] `TestExecutor_MaxSeverityForFixBelowMinSeverityRejected` asserts a specific, distinguishable contradictory-range error message
- [ ] A positive-path test confirms a valid ceiling-at-or-above-floor combination (including the equal-value boundary) loads without error

**Manual Review:**
- [ ] Code reviewed and approved

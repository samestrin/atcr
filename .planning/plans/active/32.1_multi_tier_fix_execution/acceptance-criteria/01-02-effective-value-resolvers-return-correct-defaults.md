# Acceptance Criteria: Effective-Value Resolvers Return Correct Defaults

**Related User Story:** [01: Configure a Complexity Ceiling on the Executor](../user-stories/01-configure-complexity-ceiling.md)

## Acceptance Criteria
`EffectiveMaxEstimatedMinutes()` returns the configured ceiling when set to a positive value and the `0` "no ceiling" sentinel when nil, zero, or negative; `EffectiveMaxSeverityForFix()` returns the configured value when non-empty and `""` (no ceiling) when unset — both are pure pass-through/fallback resolvers that perform no validation or normalization of their own.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go method / config package | resolver methods on `ExecutorConfig` in `internal/registry/config.go` |
| Test Framework | go test (testify assert/require) | mirrors existing resolver tests, e.g. `EffectiveMaxToolCalls`/`EffectiveFixMinSeverity` coverage |
| Key Dependencies | none new | pure in-memory struct methods, no I/O |

### Related Files (from codebase-discovery.json)
- `internal/registry/config.go` - modify: add `EffectiveMaxEstimatedMinutes() int` and `EffectiveMaxSeverityForFix() string` resolver methods on `ExecutorConfig`, placed alongside `EffectiveMaxToolCalls` (lines 227-236) and `EffectiveFixMinSeverity` (lines 238-249), following the same doc-comment structure (state the fallback rule and which later caller will consume it).
- `internal/registry/executor_config_test.go` - modify: add new test functions asserting resolver behavior for unset, explicit-zero (for the minutes ceiling), and explicit-value cases, mirroring the style of existing tests like `TestExecutor_MaxToolCallsBoundaryAccepted` (line 557).

## Happy Path Scenarios
**Scenario 1: EffectiveMaxEstimatedMinutes returns configured ceiling when set**
- **Given** an `ExecutorConfig` with `MaxEstimatedMinutes` set to a pointer to `45`
- **When** `EffectiveMaxEstimatedMinutes()` is called
- **Then** it returns `45`

**Scenario 2: EffectiveMaxSeverityForFix returns configured ceiling when set**
- **Given** an `ExecutorConfig` with `MaxSeverityForFix` set to `"HIGH"`
- **When** `EffectiveMaxSeverityForFix()` is called
- **Then** it returns `"HIGH"`

**Scenario 3: EffectiveMaxEstimatedMinutes returns "no ceiling" sentinel when unset**
- **Given** an `ExecutorConfig` with `MaxEstimatedMinutes` left as its zero value (`nil`)
- **When** `EffectiveMaxEstimatedMinutes()` is called
- **Then** it returns the documented "no ceiling" sentinel — `0` (meaning unlimited, consistent with the story's stated rule that "a ceiling of `0` or an unset field means no ceiling") — distinguishable in code from any positive configured ceiling

**Scenario 4: EffectiveMaxSeverityForFix returns "no ceiling" sentinel when unset**
- **Given** an `ExecutorConfig` with `MaxSeverityForFix` left as its zero value (`""`)
- **When** `EffectiveMaxSeverityForFix()` is called
- **Then** it returns `""` (the empty-string sentinel meaning "no severity ceiling" — every finding severity is eligible), mirroring how `EffectiveFixMinSeverity` treats its own empty case as "apply the default," except here the resolver semantic is "no ceiling" rather than "apply a default value"

## Edge Cases
**Edge Case 1: MaxEstimatedMinutes explicitly set to 0**
- **Given** an `ExecutorConfig` with `MaxEstimatedMinutes` set to a pointer to `0`
- **When** `EffectiveMaxEstimatedMinutes()` is called
- **Then** it returns `0` (no ceiling) — an explicit `0` and an unset (`nil`) field resolve identically, per the story's documented assumption ("A ceiling of `0` or an unset field means no ceiling")

**Edge Case 2: MaxEstimatedMinutes set to a negative value**
- **Given** an `ExecutorConfig` with `MaxEstimatedMinutes` set to a pointer to `-5` (a value that would be rejected by Story 4's future validation, but this AC tests the resolver in isolation, as `validateExecutor` range-checking is out of scope here)
- **When** `EffectiveMaxEstimatedMinutes()` is called
- **Then** it returns `0` (treated as "no ceiling"), consistent with the non-positive → sentinel fallback pattern already used by `EffectiveMaxToolCalls` (`*e.MaxToolCalls > 0` guard)

**Edge Case 3: MaxSeverityForFix set to a value not in the canonical rubric**
- **Given** an `ExecutorConfig` with `MaxSeverityForFix` set to `"BOGUS"` (a value that would be rejected by Story 4's future validation)
- **When** `EffectiveMaxSeverityForFix()` is called
- **Then** it returns `"BOGUS"` verbatim — the resolver performs no validation or normalization of its own; it only distinguishes "set" from "unset," mirroring `EffectiveFixMinSeverity`'s pass-through behavior for any non-empty string

## Error Conditions
- **Out of scope for this AC.** These resolver methods never return an error — they are pure value-returning functions with a documented fallback rule, matching the signatures of `EffectiveMaxToolCalls() int` and `EffectiveFixMinSeverity() string`. No error scenario applies.

## Performance Requirements
- **Response Time:** O(1) — a nil-check/pointer-dereference and a string-emptiness check, no allocation, no I/O; negligible even under high call frequency (e.g. once per finding during `generateFixes` in a later story).
- **Throughput:** N/A — in-memory struct method, not a hot loop by itself.

## Security Considerations
- **Authentication/Authorization:** N/A — pure config-value resolution, no auth surface.
- **Input Validation:** None performed here by design (Story 4 owns validation at load time). These resolvers must not silently "fix" an invalid value (e.g. clamp `"BOGUS"` to `"HIGH"`) — a later story's routing logic and Story 4's validation together are responsible for guaranteeing only valid values reach these resolvers in a loaded registry; the resolver's job is solely the unset-vs-set fallback.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** In-memory `ExecutorConfig{}` struct literals covering: zero-value (unset), explicit `0`/`""`, explicit positive/valid value, and one deliberately invalid value (to prove the resolver is a pass-through, not a validator) — no YAML fixtures or `LoadRegistry` calls required since these are direct method-call tests on struct literals.
**Mock/Stub Requirements:** None — no dependencies to mock; direct unit tests against the `ExecutorConfig` methods.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `EffectiveMaxEstimatedMinutes() int` returns the configured value when `MaxEstimatedMinutes` is set to a positive int, and `0` (no ceiling) when nil, zero, or negative
- [ ] `EffectiveMaxSeverityForFix() string` returns the configured value when `MaxSeverityForFix` is non-empty, and `""` (no ceiling) when unset
- [ ] Resolver doc comments state the fallback rule explicitly and reference that a later story's `generateFixes` routing logic is the intended consumer, mirroring the `EffectiveFixMinSeverity`/`EffectiveMaxToolCalls` comment style
- [ ] Resolvers perform no validation/normalization of out-of-range or invalid input — confirmed by a passthrough test case

**Manual Review:**
- [ ] Code reviewed and approved

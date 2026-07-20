# Acceptance Criteria: ExecutorConfig Exposes Complexity Ceiling Fields

**Related User Story:** [01: Configure a Complexity Ceiling on the Executor](../user-stories/01-configure-complexity-ceiling.md)

## Acceptance Criteria
`ExecutorConfig` loads `max_estimated_minutes` (`*int`, `omitempty`) and `max_severity_for_fix` (`string`, `omitempty`) from `atcr.yaml` without error, normalizes the severity value to canonical case, and treats both fields as absent-safe (nil/empty) when omitted from a pre-existing config — with zero validation logic added at this layer (deferred to Story 4).

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct field / config package | `internal/registry/config.go`, `ExecutorConfig` struct |
| Test Framework | go test (testify assert/require) | mirrors existing `internal/registry/executor_config_test.go` patterns |
| Key Dependencies | `gopkg.in/yaml.v3` (struct tags), none new | no new packages required |

### Related Files (from codebase-discovery.json)
- `internal/registry/config.go` - modify: add `MaxEstimatedMinutes *int` (yaml: `max_estimated_minutes,omitempty`) and `MaxSeverityForFix string` (yaml: `max_severity_for_fix,omitempty`) fields to the `ExecutorConfig` struct (currently lines 206-225), plus doc comments explaining each field's role, mirroring the `MinSeverity`/`TimeoutSecs` comment style already on the struct.
- `internal/registry/executor_config_test.go` - modify: add new test functions covering parsing of both new fields, backward-compatibility when absent, and interaction with `LoadRegistry`/`writeRegistry` helpers already defined in the file (e.g. `executorBaseProviders` constant, lines 12-22).

## Happy Path Scenarios
**Scenario 1: Both ceiling fields parsed when present**
- **Given** an `atcr.yaml` executor block with `max_estimated_minutes: 30` and `max_severity_for_fix: HIGH`
- **When** `LoadRegistry` parses the config
- **Then** `reg.Executor.MaxEstimatedMinutes` is a non-nil pointer whose dereferenced value is `30`, and `reg.Executor.MaxSeverityForFix` equals `"HIGH"`

**Scenario 2: max_severity_for_fix normalized to canonical case**
- **Given** an executor block with `max_severity_for_fix: high` (lowercase)
- **When** `LoadRegistry` parses and applies defaults
- **Then** `reg.Executor.MaxSeverityForFix` is normalized to `"HIGH"`, mirroring the existing `MinSeverity` normalization behavior (`TestExecutor_MinSeverityForFixExplicitAndNormalized`)

**Scenario 3: Fields omitted are absent-safe (backward compatibility)**
- **Given** an existing `atcr.yaml` executor block that predates this story (no `max_estimated_minutes` or `max_severity_for_fix` keys)
- **When** `LoadRegistry` parses the config
- **Then** loading succeeds with no error, `reg.Executor.MaxEstimatedMinutes` is `nil`, and `reg.Executor.MaxSeverityForFix` is `""`

## Edge Cases
**Edge Case 1: max_estimated_minutes explicitly set to 0**
- **Given** an executor block with `max_estimated_minutes: 0`
- **When** `LoadRegistry` parses the config
- **Then** `reg.Executor.MaxEstimatedMinutes` is a non-nil pointer dereferencing to `0` (distinguishable from the unset/nil case), consistent with the pointer convention used for `TimeoutSecs`/`MaxToolCalls` — the *meaning* of an explicit `0` is deferred to the effective-value resolver (AC 01-02), not this AC

**Edge Case 2: max_severity_for_fix set to empty string explicitly**
- **Given** an executor block with `max_severity_for_fix: ""`
- **When** `LoadRegistry` parses the config
- **Then** `reg.Executor.MaxSeverityForFix` is `""`, identical to the omitted case — no error is raised at the parse/struct level (range/validity checking is explicitly out of scope for this story per the parent story's Story 4 deferral)

**Edge Case 3: YAML round-trip preserves omitempty**
- **Given** an `ExecutorConfig` value with both new fields unset (nil / empty string)
- **When** the struct is marshaled back to YAML (if/where the codebase does so)
- **Then** neither `max_estimated_minutes` nor `max_severity_for_fix` keys appear in the output, per the `omitempty` tag

## Error Conditions
- **Out of scope for this AC.** Range/format validation of `max_estimated_minutes` (non-positive, above a max constant) and `max_severity_for_fix` (not in CRITICAL/HIGH/MEDIUM/LOW, or below `min_severity_for_fix`) is explicitly deferred to a later story (Story 4, `validateExecutor`). This AC covers only that the fields exist on the struct, parse to the exact configured values asserted in Scenarios 1–2 (pointer value `30`, normalized `"HIGH"`), and are backward-compatible when absent — no error conditions are asserted here beyond "parsing never fails solely because these two fields are present or absent."

## Performance Requirements
- **Response Time:** Struct field addition and YAML unmarshal add no measurable parse-time overhead (same order as the four existing pointer/string fields already on `ExecutorConfig`).
- **Throughput:** N/A — config parsing is a one-time, per-run operation, not a hot path.

## Security Considerations
- **Authentication/Authorization:** N/A — this AC touches config schema only, no auth surface.
- **Input Validation:** None performed at this layer by design (deferred to Story 4's `validateExecutor` range checks). This AC must NOT introduce any validation logic — doing so would duplicate/conflict with Story 4's scope. `MaxSeverityForFix` is a plain string field with no interpolation into prompts or shell commands, so no injection surface is introduced by merely adding the field.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Minimal YAML fixtures built on the existing `executorBaseProviders` constant (`internal/registry/executor_config_test.go:12-22`) with an appended `executor:` block setting one or both new fields; also a fixture with neither field set to prove the absent-case default.
**Mock/Stub Requirements:** None — `LoadRegistry`/`writeRegistry` test helpers already in the file are sufficient; no external providers or network calls are involved.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `ExecutorConfig` struct has `MaxEstimatedMinutes *int` (yaml: `max_estimated_minutes,omitempty`) and `MaxSeverityForFix string` (yaml: `max_severity_for_fix,omitempty`) fields with doc comments
- [x] New unit tests confirm both fields parse to the exact configured values asserted in Scenarios 1–2 when present, including case-normalization of `max_severity_for_fix`
- [x] New unit tests confirm both fields are absent-safe (nil/empty) when omitted from an existing `atcr.yaml`, with no parse error
- [x] No validation logic (range checks, allowed-value checks) is introduced in this AC — confirmed by diff review against Story 4's scope boundary

**Manual Review:**
- [ ] Code reviewed and approved

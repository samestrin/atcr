# Acceptance Criteria: Env Var and Config Opt-Outs Are OR'd, Never Overridden

**Related User Story:** [02: Telemetry Opt-Out](../user-stories/02-telemetry-opt-out.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (telemetry client construction logic) | `cmd/atcr` + `internal/telemetry` |
| Test Framework | `go test`, `testify` `assert`/`require`, table-driven tests | Test location: `cmd/atcr/main_test.go` or a new `cmd/atcr/telemetry_gate_test.go` |
| Key Dependencies | None new — combines outputs of AC 02-01 (`telemetryEnabledFromEnv`) and AC 02-02 (`ProjectConfig.Telemetry`) | |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/main.go` - modify: add a combining function (e.g. `telemetryDisabled(envEnabled bool, cfg *registry.ProjectConfig) bool`) called once at root-command construction / client-construction time (`cmd/atcr/main.go:217`), implementing strict OR-disables semantics.
- `internal/telemetry/client.go` - modify (Story 1 client): construction seam accepts the final resolved boolean (not the two raw inputs), so the client itself has no precedence logic to get wrong.
- `cmd/atcr/main_test.go` - modify/create: table test covering all four combinations of `{env unset/0} x {config true/false}` asserting disabled wins whenever either source says disabled.
- `cmd/atcr/review_test.go` - modify: integration-level assertion that the OR combination holds for a real `review` invocation (env unset + config false = disabled; env `0` + config true = disabled).

## Happy Path Scenarios
**Scenario 1: config says disabled, env var unset — disabled wins**
- **Given** `.atcr/config.yaml` has `telemetry: false` and `ATCR_TELEMETRY` is unset
- **When** `atcr review` runs against a mock telemetry endpoint
- **Then** zero HTTP requests are observed (config's `false` is honored with no env var needed)

**Scenario 2: env var says disabled, config says enabled — disabled wins**
- **Given** `ATCR_TELEMETRY=0` is set and `.atcr/config.yaml` has `telemetry: true`
- **When** `atcr review` runs against a mock telemetry endpoint
- **Then** zero HTTP requests are observed (env var's `0` is honored even though config says enabled — this is the scenario the story's second Potential Risk calls out explicitly: config must never be treated as authoritative over an explicit env-var opt-out)

**Scenario 3: both sources say enabled — telemetry fires**
- **Given** `ATCR_TELEMETRY` is unset (defaults enabled) and `.atcr/config.yaml` has `telemetry: true` (or the field is absent)
- **When** `atcr review` runs
- **Then** the telemetry ping is sent normally

## Edge Cases
**Edge Case 1: full four-way matrix**
- **Given** the combinations `{env unset, env=0} x {config true, config false}`
- **When** each combination is evaluated by the combining function
- **Then** the truth table is: (unset, true)=enabled, (unset, false)=disabled, (0, true)=disabled, (0, false)=disabled — i.e. enabled only when BOTH sources say enabled, matching strict OR-disables (equivalently AND-enables) semantics

**Edge Case 2: config field absent (`nil`) is treated as "not disabling"**
- **Given** `.atcr/config.yaml` has no `telemetry` key (`ProjectConfig.Telemetry == nil`)
- **And** `ATCR_TELEMETRY=0` is set
- **When** the combining function evaluates
- **Then** the env var alone is sufficient to disable — a `nil` config field must not be misread as an implicit "config says enabled, which could theoretically be given precedence"; it simply contributes nothing to the OR and the env var's `false` still wins

**Edge Case 3: no third surface can re-enable within the same invocation**
- **Given** telemetry is disabled by either surface
- **When** the same `atcr` invocation processes any other flags or config values
- **Then** no other flag, config key, or code path in the invocation can flip the resolved state back to enabled — the combining function's output is computed once and passed down, not re-evaluated or overridden downstream

## Error Conditions
**Error Scenario 1: N/A — pure boolean combination, no error path**
- The combining function is total (defined for all four input combinations) and cannot itself error; malformed inputs are already normalized to `bool`/`nil` by AC 02-01 and AC 02-02 before reaching this function.
- HTTP status / error code: not applicable

## Performance Requirements
- **Response Time:** The OR combination is a single boolean expression evaluated once per process; negligible (< 1 microsecond), no measurable effect on command latency.
- **Throughput:** N/A.
- **Strictness requirement:** The combining function must be called exactly once per invocation, at the same construction point identified in AC 02-01, so there is no window where a subcommand could observe a different resolved state than another subcommand in the same process.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** N/A — inputs are already-validated booleans/nil from AC 02-01 and AC 02-02; this AC is pure logic composition with no external input surface.
- **Privacy Guarantee:** This is the core trust mechanism referenced in the epic's AC3 ("`ATCR_TELEMETRY=0` strictly disables all background telemetry") — a regression here (config silently overriding an env-var opt-out) is a privacy-critical defect, not merely a functional bug. Test coverage for this AC should be treated as a release gate.

## Test Implementation Guidance
**Test Type:** UNIT (table-driven truth-table test of the combining function) + INTEGRATION (real `review` invocation for at least the two "disagreement" rows of the matrix — Scenario 1 and Scenario 2 above)
**Test Data Requirements:** Table of `{envValue string, configTelemetry *bool, wantDisabled bool}` covering all four matrix cells plus the `nil`-config edge case; helper to construct a `*bool` pointer for `true`/`false`/`nil` cases.
**Mock/Stub Requirements:** `t.Setenv` for the env var axis; a temp `.atcr/config.yaml` fixture (or direct `ProjectConfig` struct construction bypassing the file) for the config axis; `httptest.Server` mock telemetry endpoint for the two integration-level disagreement scenarios.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A single combining function resolves the final enabled/disabled state from both surfaces, called once per invocation
- [ ] All four combinations of `{env unset/0} x {config true/false}` are covered by a table test, asserting disabled wins whenever either source says disabled
- [ ] The "config=true overrides env=0" regression scenario is explicitly tested and asserted to NOT re-enable telemetry
- [ ] `nil`/absent config field does not accidentally count as an "enabled" vote that could out-rank a disabling env var (it is simply neutral)

**Manual Review:**
- [ ] Code reviewed and approved

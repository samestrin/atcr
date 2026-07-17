# Acceptance Criteria: Pure Four-Combination Gate, Independent of `telemetryGate`/`resolveSyncCloud`

**Related User Story:** [02: Independent Opt-In Gate for Quality-Signal Transmission](../user-stories/02-quality-signal-opt-in-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (pure combining function + I/O wrapper) | `cmd/atcr` |
| Test Framework | `go test`, `testify` `assert`, table-driven tests | Test location: `cmd/atcr/qualitysignal_test.go`, mirroring `TestTelemetryEnabled_FourWayMatrix` in `cmd/atcr/telemetry_gate_test.go:90-109` |
| Key Dependencies | None new — combines a live env-var read with a persisted config read, structurally independent of `telemetryGate`/`resolveSyncCloud` | |

## Related Files
- `cmd/atcr/qualitysignal.go` - create: `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool`, a total pure function evaluating the same OR-disables (AND-enables) shape as `telemetryEnabled` (`cmd/atcr/telemetry.go:37-39`), plus `qualitySignalGate() bool` reading `registry.LoadQualitySignalSetting(".")` — neither reads nor calls `telemetryGate()` or `resolveSyncCloud()`.
- `cmd/atcr/telemetry.go` - reference only (no shared state): `telemetryEnabled`/`telemetryGate` (lines 37-66) are the structural pattern being mirrored, not a dependency to import or branch on.
- `cmd/atcr/cloudsync.go` - reference only (no shared state): `resolveSyncCloud` (lines 33-49) is the second precedent for an independent, non-overriding opt-in surface; the quality-signal gate must not read `syncCloudPlan` or any of its fields.
- `cmd/atcr/qualitysignal_test.go` - create: exhaustive four-combination table test plus a cross-feature independence test (`telemetry: false` + `quality_signal: true` must still resolve quality-signal enabled, and vice versa).

## Happy Path Scenarios
**Scenario 1: full four-way matrix resolves per the OR-disables truth table**
- **Given** the combinations `{envEnabled: true/false} x {cfgQualitySignal: nil/&true/&false}` (six meaningful cells, matching `telemetryEnabled`'s existing matrix shape)
- **When** `qualitySignalEnabled` is evaluated for each cell
- **Then** the results are: `(true, nil)=true`, `(true, &true)=true`, `(true, &false)=false`, `(false, nil)=false`, `(false, &true)=false`, `(false, &false)=false` — enabled only when both the env axis permits it AND the config does not disable it

**Scenario 2: quality-signal state is independent of `telemetry`'s persisted value**
- **Given** `.atcr/config.yaml` has `telemetry: false` and `quality_signal: true`
- **When** `qualitySignalGate()` and `telemetryGate()` are both called
- **Then** `qualitySignalGate()` returns `true` and `telemetryGate()` returns `false` — the two surfaces disagree and neither one's persisted key influences the other's resolution

**Scenario 3: quality-signal state is independent of `--sync-cloud`/`ATCR_API_KEY` presence**
- **Given** `ATCR_API_KEY` is set and a hypothetical `--sync-cloud` flag would resolve `resolveSyncCloud()` to enabled
- **And** no quality-signal env var is set and no `quality_signal` config key is present
- **When** `qualitySignalGate()` is called
- **Then** it returns `false` — the presence of a valid API key and an enabled cloud-sync plan has no bearing on the quality-signal gate's result

## Edge Cases
**Edge Case 1: `nil` config field is neutral, never an implicit vote either direction**
- **Given** `cfgQualitySignal` is `nil` (key absent from config)
- **And** `envEnabled` is `true`
- **When** `qualitySignalEnabled` evaluates
- **Then** the result is `true` — a `nil` field contributes nothing to the OR and cannot out-rank a permitting env var, matching `telemetryEnabled`'s existing nil-neutrality contract

**Edge Case 2: env-disabled beats config-enabled (env wins, no override)**
- **Given** `envEnabled` is `false`
- **And** `cfgQualitySignal` is `&true`
- **When** `qualitySignalEnabled` evaluates
- **Then** the result is `false` — config can never re-enable what an env-var opt-out disabled, mirroring `telemetryEnabled`'s asymmetric precedence

**Edge Case 3: the two gates are called via structurally separate code paths, not a shared helper**
- **Given** `qualitySignalGate()` and `telemetryGate()` are both present in the same binary
- **When** the source is inspected (a static/lint-level check, not a runtime scenario)
- **Then** neither function's implementation calls the other, shares a package-level variable, or funnels through a common precedence table — each is a fully self-contained pure function plus I/O wrapper pair

## Error Conditions
**Error Scenario 1: N/A — pure boolean combination, no error path**
- `qualitySignalEnabled` is total (defined for every combination of its two inputs) and cannot itself error; a malformed persisted value is normalized to an error at the `LoadQualitySignalSetting` layer before it ever reaches this function (covered by AC 02-03).
- HTTP status / error code: not applicable

## Performance Requirements
- **Response Time:** The combination is a single boolean expression evaluated once per gate check; negligible (<1 microsecond), no measurable effect on command latency.
- **Throughput:** N/A.
- **Strictness requirement:** `qualitySignalGate()` must not read or evaluate `telemetryGate()`/`resolveSyncCloud()` as part of its own resolution — verified by a test asserting divergent results across the two surfaces are both achievable simultaneously (Scenario 2).

## Security Considerations
- **Authentication/Authorization:** N/A — pure local boolean logic; no credentials involved.
- **Input Validation:** Inputs are already-normalized `bool`/`*bool` values by the time they reach this function; no external input surface here.
- **Privacy Guarantee:** This is the core independence mechanism the story's Constraints section and its first Potential Risk call out explicitly — coupling this gate's result to `telemetryGate()` or `resolveSyncCloud()` would silently grant or revoke quality-signal consent via an unrelated feature's setting. Regression coverage here should be treated as a release gate, consistent with the equivalent Epic 28.0 precedent (AC 02-03 there).

## Test Implementation Guidance
**Test Type:** UNIT (table-driven truth-table test) + a targeted independence test combining both persisted keys in one config fixture
**Test Data Requirements:** Table of `{envEnabled bool, cfg *bool, want bool}` covering all six matrix cells (mirroring `TestTelemetryEnabled_FourWayMatrix`'s six-case table); a single `.atcr/config.yaml` fixture with both `telemetry` and `quality_signal` keys set to opposing values for the independence assertion.
**Mock/Stub Requirements:** `t.Setenv`/`os.Unsetenv` for the env-var axis on `qualitySignalGate()`; a temp-dir config fixture (`t.TempDir()` + `os.WriteFile`) for the config axis; no network mocking needed (pure logic + local file read).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `qualitySignalEnabled` is a pure, total function with all six matrix cells covered by a table test
- [ ] A test proves `telemetry: false` + `quality_signal: true` resolves quality-signal enabled (and the converse), demonstrating independence
- [ ] Neither `qualitySignalGate()` nor `qualitySignalEnabled` calls, imports as a dependency, or shares state with `telemetryGate()`/`telemetryEnabled()`/`resolveSyncCloud()`

**Manual Review:**
- [ ] Code reviewed and approved

# Acceptance Criteria: Quality Signal Resolves Disabled With No Env Var and No Persisted Config

**Related User Story:** [02: Independent Opt-In Gate for Quality-Signal Transmission](../user-stories/02-quality-signal-opt-in-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI config gate) | `cmd/atcr` |
| Test Framework | `go test` (standard library `testing`), `testify` `assert`/`require` | Mirrors `cmd/atcr/telemetry_gate_test.go` conventions |
| Key Dependencies | `os` (`Getenv`), `github.com/samestrin/atcr/internal/registry` | No new third-party dependency |

## Related Files
- `cmd/atcr/qualitysignal.go` - create: `qualitySignalEnabled(envEnabled bool, cfgQualitySignal *bool) bool` pure combining function plus `qualitySignalGate() bool` I/O wrapper, structurally mirroring `telemetryEnabled`/`telemetryGate` in `cmd/atcr/telemetry.go:37-66`.
- `internal/registry/quality_signal_setting.go` - create: `LoadQualitySignalSetting(root string) (*bool, error)` mirroring `LoadTelemetrySetting` in `internal/registry/telemetry_setting.go:27-46` — permissive decode of only the `quality_signal` key, `(nil, nil)` when file absent or key absent.
- `cmd/atcr/qualitysignal_test.go` - create: unit test asserting the gate resolves `false` when `ATCR_QUALITY_SIGNAL` (or equivalent) is unset and no `.atcr/config.yaml` `quality_signal` key is present.
- `internal/registry/quality_signal_setting_test.go` - create: unit test asserting `LoadQualitySignalSetting` returns `(nil, nil)` for an absent file and for a present file lacking the `quality_signal` key.

## Happy Path Scenarios
**Scenario 1: no env var, no config file at all — gate resolves disabled**
- **Given** no quality-signal env var is set in the process environment
- **And** no `.atcr/config.yaml` file exists in the project root
- **When** `qualitySignalGate()` is called
- **Then** it returns `false` — quality-signal transmission never fires by default

**Scenario 2: no env var, config file present but without the `quality_signal` key — gate resolves disabled**
- **Given** no quality-signal env var is set
- **And** `.atcr/config.yaml` exists with other keys (e.g. `agents: [bruce]`, `telemetry: true`) but no `quality_signal` key
- **When** `qualitySignalGate()` is called
- **Then** it returns `false` — an absent key is neutral, not an implicit opt-in, and the pre-existing `telemetry: true` value has no bearing

**Scenario 3: `atcr review`/`atcr reconcile` run with the gate disabled schedules no aggregation or send path**
- **Given** the gate resolves disabled per Scenario 1 or 2
- **When** a review or reconcile run completes
- **Then** no quality-signal payload is built and no network call is attempted (this story delivers only the gate; the call site itself is out of scope, but the gate's false result must be observable and safe to wire a future no-op against)

## Edge Cases
**Edge Case 1: empty (0-byte) config file**
- **Given** `.atcr/config.yaml` exists but is empty (0 bytes)
- **When** `LoadQualitySignalSetting` reads it
- **Then** it returns `(nil, nil)` (neutral), matching `LoadTelemetrySetting`'s existing empty-file contract — no parse error on a stub file

**Edge Case 2: unrelated keys present, `quality_signal` absent**
- **Given** `.atcr/config.yaml` contains only unrelated keys (`payload_mode: blocks`)
- **When** `LoadQualitySignalSetting` reads it
- **Then** it returns `(nil, nil)` — the reader is permissive of sibling keys it does not recognize, matching `LoadTelemetrySetting`'s tolerance of unrelated fields

**Edge Case 3: gate is evaluated fresh per invocation, not cached across runs**
- **Given** a prior invocation resolved the gate disabled
- **When** a later invocation runs after `.atcr/config.yaml` was NOT modified
- **Then** the gate still resolves disabled — there is no stale in-process cache that could resolve a different answer without a config or env change

## Error Conditions
**Error Scenario 1: N/A for the default-disabled path — no error to surface**
- The absent-env/absent-config default-disabled path never errors; a read failure or malformed value is covered by AC 02-03, not this AC.
- HTTP status / error code: not applicable (no user-facing error surface for this AC)

## Performance Requirements
- **Response Time:** The default-disabled resolution is a single `os.Getenv` read plus one `os.ReadFile`/YAML-unmarshal-of-nothing (or a stat-not-exist short circuit); negligible (<1ms), no measurable added latency to `review`/`reconcile`.
- **Throughput:** N/A — this AC only gates whether a quality-signal path can ever be entered; it does not affect review/reconcile throughput.
- **Strictness requirement:** No goroutine, no HTTP client, and no payload allocation may occur when the gate resolves disabled — the false result must be observable at construction time, not merely as a post-hoc no-op deeper in a call chain.

## Security Considerations
- **Authentication/Authorization:** N/A — this is a local, unauthenticated boolean read; no credentials are involved.
- **Input Validation:** No user input is parsed on the default-disabled path (env unset, config key absent); nothing to validate beyond presence checks.
- **Privacy Guarantee:** This AC is the epic's AC1 floor ("nothing sent by default") for the quality-signal payload specifically — a regression here (resolving enabled with no explicit opt-in) is a privacy-critical defect, not a functional bug. Test coverage for this AC should be treated as a release gate, matching the precedent set by AC 02-03 of Epic 28.0's telemetry opt-out.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A hermetic temp directory (or `t.TempDir()`) with no `.atcr/config.yaml`, a variant with an empty `.atcr/config.yaml`, and a variant with a config file containing unrelated keys only; `t.Setenv`/`os.Unsetenv` for the env-var axis.
**Mock/Stub Requirements:** None — pure function plus a file-system read against a temp fixture; no network or process mocking needed.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `qualitySignalGate()` returns `false` when no env var is set and no `.atcr/config.yaml` `quality_signal` key exists
- [ ] `LoadQualitySignalSetting` returns `(nil, nil)` for an absent file, an empty file, and a file lacking the key
- [ ] No goroutine, HTTP client, or payload allocation occurs on the disabled path

**Manual Review:**
- [ ] Code reviewed and approved

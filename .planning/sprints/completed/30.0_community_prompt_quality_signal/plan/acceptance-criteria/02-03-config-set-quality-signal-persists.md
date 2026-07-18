# Acceptance Criteria: `atcr config set quality_signal <bool>` Persists Atomically, Fails Safe on Corruption

**Related User Story:** [02: Independent Opt-In Gate for Quality-Signal Transmission](../user-stories/02-quality-signal-opt-in-gate.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (CLI subcommand + config persistence) | `cmd/atcr` (`config.go`) + `internal/registry` |
| Test Framework | `go test`, `testify` `assert`/`require` | Mirrors `cmd/atcr/config_test.go` and `internal/registry/telemetry_setting_test.go` conventions |
| Key Dependencies | `strconv.ParseBool`, `gopkg.in/yaml.v3`, `github.com/spf13/cobra` | No new third-party dependency |

## Related Files
- `cmd/atcr/config.go` - modify: extend `runConfigSet`'s key allowlist (`cmd/atcr/config.go:59`, today the single literal `"telemetry"` check) to also accept `"quality_signal"`, dispatching to `registry.SetQualitySignalSetting`/`LoadQualitySignalSetting` for that key; update `newConfigSetCmd`'s `Long` help text to document the second key.
- `internal/registry/quality_signal_setting.go` - create: `SetQualitySignalSetting(root string, enabled bool) error` mirroring `SetTelemetrySetting` (`internal/registry/telemetry_setting.go:53-133`) verbatim — reuses `withConfigLock`, `configMapping`, `setMappingBool` (already key-agnostic), same atomic temp-file + fsync + rename + symlink-rejection behavior.
- `internal/registry/quality_signal_setting_test.go` - create: round-trip, sibling-key-preservation, malformed-value, missing-file, and symlink-rejection tests mirroring `TestSetTelemetrySetting` and `TestSetTelemetrySetting_SymlinkRejected` (`internal/registry/telemetry_setting_test.go:95-167`).
- `cmd/atcr/config_test.go` - modify: add `TestConfigSetQualitySignal_*` tests mirroring the existing `TestConfigSetTelemetry_*` tests (lines 47-92), plus a test asserting `atcr config set quality_signal true` leaves an existing `telemetry` key untouched and vice versa.

### Related Files (from codebase-discovery.json)

- `cmd/atcr/config.go` - update: extend the settable-key allowlist at `:59` (today `key != "telemetry"`) to admit `"quality_signal"`; update `newConfigSetCmd`'s `Long` help text
- `internal/registry/quality_signal_setting.go` - create: `SetQualitySignalSetting`, mirroring `internal/registry/telemetry_setting.go:53` (`SetTelemetrySetting`) and reusing `withConfigLock` (`:144`), `configMapping` (`:215`), `setMappingBool` (`:233`)
- `internal/registry/quality_signal_setting_test.go` - create: round-trip, sibling-key-preservation, malformed-value, and symlink-rejection tests
- `cmd/atcr/config_test.go` - update: `TestConfigSetQualitySignal_*` cases alongside the existing `TestConfigSetTelemetry_*` tests

## Happy Path Scenarios
**Scenario 1: `atcr config set quality_signal true` persists the key**
- **Given** `.atcr/config.yaml` exists with `agents: [bruce]` and no `quality_signal` key
- **When** `atcr config set quality_signal true` is run
- **Then** the command exits 0, and `registry.LoadQualitySignalSetting(".")` returns `(&true, nil)`

**Scenario 2: `atcr config set quality_signal false` persists the key and round-trips**
- **Given** `.atcr/config.yaml` has `quality_signal: true`
- **When** `atcr config set quality_signal false` is run
- **Then** the command exits 0, and a subsequent `LoadQualitySignalSetting` call returns `(&false, nil)`

**Scenario 3: setting `quality_signal` leaves the `telemetry` key and all other keys untouched**
- **Given** `.atcr/config.yaml` has `agents: [bruce]`, `telemetry: false`, `payload_mode: blocks`
- **When** `atcr config set quality_signal true` is run
- **Then** `quality_signal: true` is added, and `agents`, `telemetry`, and `payload_mode` retain their exact prior values (surgical yaml-node edit, not a rewrite)

**Scenario 4: `atcr config set telemetry <bool>` continues to work unchanged and leaves `quality_signal` untouched**
- **Given** `.atcr/config.yaml` has `quality_signal: true`
- **When** `atcr config set telemetry false` is run
- **Then** `telemetry: false` is set and `quality_signal: true` survives unchanged — the allowlist extension does not regress the existing single-key behavior

## Edge Cases
**Edge Case 1: `strconv.ParseBool` vocabulary accepted for the value**
- **Given** `.atcr/config.yaml` exists with `agents: [bruce]`
- **When** `atcr config set quality_signal 1` and later `atcr config set quality_signal False` are run
- **Then** both are accepted, persisting `true` and `false` respectively (same vocabulary as the existing `telemetry` key: `0/1`, `t/f`, `True/False`, `TRUE/FALSE`)

**Edge Case 2: repo-subdirectory resolution**
- **Given** the current working directory is a subdirectory of a repo whose root contains `.atcr/config.yaml`
- **When** `atcr config set quality_signal true` is run from that subdirectory
- **Then** the key is persisted at the discovered repo root's `.atcr/config.yaml`, mirroring `TestConfigSetTelemetry_ResolvesRepoRoot` (`cmd/atcr/config_test.go:138-154`)

**Edge Case 3: idempotent repeated sets**
- **Given** `.atcr/config.yaml` has `quality_signal: false`
- **When** `atcr config set quality_signal false` is run twice in a row
- **Then** both invocations succeed and the persisted value remains `false` with no corruption or duplicate key insertion

**Edge Case 4: empty (0-byte) existing config accepts the new key**
- **Given** `.atcr/config.yaml` exists but is empty
- **When** `atcr config set quality_signal true` is run
- **Then** the command succeeds and synthesizes a minimal mapping containing `quality_signal: true`, matching `configMapping`'s existing empty-document handling

## Error Conditions
**Error Scenario 1: unknown config key rejected**
- **Given** the config-key allowlist is exactly `{"telemetry", "quality_signal"}`
- **When** `atcr config set foo bar` is run
- Error message: `unsupported config key "foo": only "telemetry" and "quality_signal" are supported`
- HTTP status / error code: usage error, exit code 2

**Error Scenario 2: non-boolean value rejected**
- **Given** `.atcr/config.yaml` exists
- **When** `atcr config set quality_signal maybe` is run
- Error message: `invalid value "maybe" for quality_signal: must be a boolean (true/false/1/0)`
- HTTP status / error code: usage error, exit code 2

**Error Scenario 3: missing config file is an I/O error, not a usage error**
- **Given** no `.atcr/config.yaml` exists in the project
- **When** `atcr config set quality_signal true` is run
- Error message: wrapped `read .atcr/config.yaml: <os error>` (config set never silently creates the file)
- HTTP status / error code: environment/I-O error, exit code 1

**Error Scenario 4: malformed persisted `quality_signal` value fails safe to disabled, never silently re-enables**
- **Given** `.atcr/config.yaml` has `quality_signal: maybe` (corrupted, e.g. by manual hand-edit)
- **When** `qualitySignalGate()` is called (via `LoadQualitySignalSetting`)
- **Then** `qualitySignalGate()` returns `false` (fails safe), while `LoadQualitySignalSetting` itself surfaces a non-nil error to any caller that checks it directly — a corrupt value must never be silently coerced to `true`
- HTTP status / error code: on the review/reconcile call path this also surfaces loudly via the same strict-load mechanism `telemetryGate()` relies on, aborting before any send is reached

**Error Scenario 5: symlinked config is rejected, never silently severed**
- **Given** `.atcr/config.yaml` is a symlink to a real file elsewhere
- **When** `atcr config set quality_signal true` is run
- Error message: contains `"symlink"` (mirroring `SetTelemetrySetting`'s existing rejection, e.g. `config %s: symlinked configs are unsupported`)
- HTTP status / error code: environment/I-O error, exit code 1; the symlink itself must remain intact after the rejected write

## Performance Requirements
- **Response Time:** A single mkdir-lock acquisition (typically instantaneous, uncontended) plus one read-modify-write of `.atcr/config.yaml` (temp write, fsync, rename, dir fsync); sub-10ms for a typical small config file, no perceptible CLI latency.
- **Throughput:** N/A — `config set` is a one-shot administrative command, not a hot path.
- **Concurrency:** The mkdir-based `config.lock` serializes concurrent `config set` invocations (any key) exactly as it does today for `telemetry`; no new lock mechanism, no risk of a lost update between a `quality_signal` set and a concurrent `telemetry` set.

## Security Considerations
- **Authentication/Authorization:** N/A — this is a local, unauthenticated project-config mutation; no credentials involved.
- **Input Validation:** The key is validated against an explicit two-entry allowlist (`"telemetry"`, `"quality_signal"`), not a loosened prefix or fuzzy match — an unrecognized key is always rejected as a usage error. The value is validated strictly via `strconv.ParseBool`; no arbitrary string is ever written to the YAML value node.
- **Privacy Guarantee:** A malformed persisted value failing safe to disabled (Error Scenario 4) is the same privacy-critical contract as `LoadTelemetrySetting`'s existing behavior — a corrupted config file must never be interpretable as consent to transmit. Regression coverage here should be treated as a release gate.

## Test Implementation Guidance
**Test Type:** UNIT (`SetQualitySignalSetting`/`LoadQualitySignalSetting` round-trip, sibling-key preservation, malformed value, missing file, symlink rejection) + INTEGRATION (CLI-level `atcr config set quality_signal <value>` via the cobra command tree, mirroring `execConfig` in `cmd/atcr/config_test.go:20-29`)
**Test Data Requirements:** Config fixtures with `quality_signal` absent/true/false/malformed/symlinked, combined with a pre-existing `telemetry` key at an opposing value to prove no cross-key interference; a table of `strconv.ParseBool`-vocabulary strings (`"1"`, `"True"`, `"0"`, `"FALSE"`) for the value-parsing edge case.
**Mock/Stub Requirements:** `t.TempDir()` + `isolate(t)` (existing test helper) for hermetic `.atcr/config.yaml` fixtures; no network or process mocking needed — this AC is pure local file I/O plus CLI argument parsing.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `atcr config set quality_signal <true|false>` is accepted, persists via the atomic mkdir-lock write path, and round-trips through `LoadQualitySignalSetting`
- [x] Setting `quality_signal` leaves `telemetry` (and all other keys) untouched, and vice versa
- [x] An unknown key is still rejected as a usage error (exit 2) after the allowlist extension to two entries
- [x] A malformed persisted `quality_signal` value fails safe to disabled (never silently re-enables) and surfaces a loud error from `LoadQualitySignalSetting`

**Manual Review:**
- [ ] Code reviewed and approved

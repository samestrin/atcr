# Acceptance Criteria: Allowlisted `quality_signal.go` Payload Type with Locking Regression Test

**Related User Story:** [01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters](../user-stories/01-aggregate-per-persona-model-dismissal-counters.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct type (`internal/telemetry`) | New allowlisted payload, sibling of `internal/telemetry/event.go`'s `Event` |
| Test Framework | Go `testing` + `encoding/json` | Mirrors `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` in `internal/telemetry/client_test.go` |
| Key Dependencies | `internal/scorecard.HashPersonaID` | Reused pseudonymization primitive at the payload-construction boundary |

## Related Files
- `internal/telemetry/quality_signal.go` - create: new `QualitySignal` struct with exactly 4 fixed fields — `persona_id_hash`, `model`, `dismissed_count`, `confirmed_count` (persona identifier, model, dismissed count, confirmed count) — no `omitempty` on any field, following `Event`'s exact no-omitempty pattern
- `internal/telemetry/quality_signal_test.go` - create: `TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys` mirroring `client_test.go`'s locking regression test structure (marshal, unmarshal to `map[string]any`, assert `len(m) == 4` and exact key set)
- `internal/telemetry/event.go` - reference only: the exact no-`omitempty`, fixed-field-count pattern this new type must replicate
- `internal/scorecard/telemetry.go` - reference only: `HashPersonaID` (pseudonymization) and `TelemetryPersonaRecord`'s split between internal aggregation (raw persona name) and outbound payload construction (hashed) — the model this AC's payload-construction function follows
- `internal/localdebt/qualitysignal.go` - reference only (depends on AC 01-01): the aggregation function whose output rows are converted into `QualitySignal` payload values

### Related Files (from codebase-discovery.json)

- `internal/telemetry/quality_signal.go` - create: allowlisted `QualitySignal` struct (exactly 4 fixed fields, no `omitempty`), sibling of `internal/telemetry/event.go`'s `Event`; persona identifier hashed via `internal/scorecard/telemetry.go:26` (`HashPersonaID`) at the construction boundary (`:55` `NewTelemetryPersonaRecord` split)
- `internal/telemetry/quality_signal_test.go` - create: locking allowlist regression test mirroring `internal/telemetry/client_test.go:126` (`TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`)

## Happy Path Scenarios
**Scenario 1: Payload struct has exactly 4 allowlisted keys**
- **Given** a `QualitySignal` value built from an aggregated `(persona, model, dismissed, confirmed)` row
- **When** it is marshaled to JSON and unmarshaled into a generic `map[string]any`
- **Then** the map has exactly 4 keys, and every key is one of the allowlisted set: `persona_id_hash`, `model`, `dismissed_count`, `confirmed_count` (snake_case JSON tags, matching the `Event` payload's existing key convention)

**Scenario 2: Zero-value struct still serializes all 4 keys**
- **Given** a zero-value `QualitySignal{}`
- **When** marshaled to JSON
- **Then** all 4 keys are still present in the output (no `omitempty` causes a key to vanish), matching `Event`'s documented zero-value behavior exercised by `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys`'s `{}` case

**Scenario 3: Persona identifier is hashed at construction, not raw**
- **Given** an aggregated row carrying a raw persona name (e.g. `"security-reviewer"`)
- **When** a `QualitySignal` is constructed from that row via the payload-construction function (analogous to `NewTelemetryPersonaRecord`)
- **Then** the resulting `QualitySignal`'s persona field carries `HashPersonaID(rawPersona)`, never the raw persona name in cleartext

## Edge Cases
**Edge Case 1: A future accidental field addition fails the test immediately**
- **Given** a hypothetical future code change adds a 5th field to `QualitySignal` (e.g. a file path or finding excerpt)
- **When** the locking regression test runs
- **Then** it fails immediately with `len(m) != 4`, exactly as `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` does for `Event` — this is the primary privacy-regression guard for this AC

**Edge Case 2: Dismissed/confirmed counts of zero**
- **Given** an aggregated row where one of the counts is legitimately 0 (e.g. a persona+model pair with only confirmations, no dismissals)
- **When** converted to `QualitySignal` and marshaled
- **Then** the zero count still appears as its own key with value `0` (no `omitempty` drops it) — a maintainer report must be able to distinguish "zero dismissals" from "field absent"

## Error Conditions
**Error Scenario 1: Not applicable**
- `QualitySignal` is a plain data struct with no validation or error-returning constructor of its own in this story's scope (validation of the source aggregation is covered by ACs 01-01 through 01-04); marshaling a well-formed Go struct to JSON cannot fail.

## Performance Requirements
- **Response Time:** Struct-to-JSON marshaling is O(1) per row; converting a full aggregation result (k rows) to `[]QualitySignal` is O(k).
- **Throughput:** No network I/O in this story's scope — this AC produces only the allowlisted type and its construction function; the actual send is out of scope (per the story's Constraints section) and belongs to a later story.

## Security Considerations
- **Authentication/Authorization:** Not applicable — this is a data-shape contract, not a network endpoint.
- **Input Validation:** This is the core privacy guarantee of the entire epic: the struct's field set must be physically incapable of carrying code, file paths, problem/fix text, or justification — enforced structurally (fixed fields, no embedding of `localdebt.Record` or `Event`) and behaviorally (the locking regression test). Persona identifiers must be hashed via `HashPersonaID`, never carried raw, matching the existing `TelemetryPersonaRecord` precedent.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A handful of `QualitySignal` values: populated, zero-value, and one built via the payload-construction function from a raw-persona aggregated row (to assert hashing).
**Mock/Stub Requirements:** None — pure struct/marshal test, no HTTP client or server needed (unlike `client_test.go`'s `Send` tests, this AC covers only the payload shape, not transport).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/telemetry/...`)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `QualitySignal` has exactly 4 fields, all without `omitempty`
- [x] `TestQualitySignal_PayloadHasExactlyFourAllowlistedKeys` fails if a 5th field is ever added
- [x] The persona-identifier field is populated via `HashPersonaID`, never a raw persona name
- [x] Zero-value counts serialize as `0`, not omitted

**Manual Review:**
- [ ] Code reviewed and approved

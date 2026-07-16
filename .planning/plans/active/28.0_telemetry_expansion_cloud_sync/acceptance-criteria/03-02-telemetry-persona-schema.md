# Acceptance Criteria: Dedicated Telemetry Persona Schema Type

**Related User Story:** [03: Persona ID Hashing for the Persona Leaderboard](../user-stories/03-persona-id-hashing-for-leaderboard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct + constructor function (`internal/scorecard`) | New telemetry-scoped schema, separate from `PublicRecord` |
| Test Framework | `go test` + `testify` (`assert`/`require`) | |
| Key Dependencies | `encoding/json` (Go stdlib) for the schema's JSON tags | Consumed later by the `--sync-cloud` payload story (Story 4) |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/telemetry.go` - create: defines the telemetry-scoped record type (e.g. `TelemetryPersonaRecord`) carrying `PersonaIDHash` (and any other Persona Leaderboard fields needed, e.g. `Model`), plus a constructor (e.g. `NewTelemetryPersonaRecord(r Record) TelemetryPersonaRecord`) that populates it using `HashPersonaID` (AC 03-01).
- `internal/scorecard/export.go` - reference only (not modified by this AC): `PublicRecord` (`internal/scorecard/export.go:35`) is the allowlist this new type must remain visibly and structurally distinct from — no shared struct embedding, no field aliasing.
- `internal/scorecard/scorecard.go` - reference only (not modified by this AC): `Record` (`internal/scorecard/scorecard.go:52`) is the input the constructor reads from; no new field is added to `Record` itself.

## Happy Path Scenarios
**Scenario 1: Build a telemetry record from a scorecard Record**
- **Given** a `Record` with `Reviewer = "bruce"` and `Model = "claude-sonnet-4-6"`
- **When** the telemetry schema's constructor builds a `TelemetryPersonaRecord` from it
- **Then** the resulting value's persona-hash field equals `HashPersonaID("bruce")` and the value contains no field carrying the raw string `"bruce"`

**Scenario 2: JSON-serializable and self-contained**
- **Given** a populated `TelemetryPersonaRecord`
- **When** it is marshaled with `encoding/json`
- **Then** the resulting JSON object's keys are drawn only from this new schema (e.g. `persona_id_hash`, `model`) and never include `persona`, `reviewer`, `run_id`, or any `PublicRecord` field name verbatim in a way that could be confused with the Epic 10.0 export shape

## Edge Cases
**Edge Case 1: Struct is not structurally assignable to PublicRecord**
- **Given** the Go type definitions of `TelemetryPersonaRecord` and `PublicRecord`
- **When** compiled together
- **Then** the two types are distinct, non-identical struct types (different field sets/names) so a future accidental `var pr PublicRecord = telemetryRecord` (direct assignment) does not type-check — this is enforced structurally by the type definitions, not by a runtime check

**Edge Case 2: Model field passthrough (non-PII)**
- **Given** the schema also needs the reviewer's `Model` for leaderboard aggregation (per the epic's "aggregate which prompts are empirically the most effective" goal)
- **When** the constructor populates the schema
- **Then** `Model` is copied through as-is (not hashed) since it is not a persona identity field and is already part of the existing public allowlist elsewhere in the codebase — only the persona/reviewer identity is hashed

## Error Conditions
**Error Scenario 1: Constructor given a zero-value Record**
- **Given** an empty `Record{}` (all fields zero-valued, `Reviewer == ""`)
- **When** the constructor builds a `TelemetryPersonaRecord`
- **Then** it does not error or panic — it produces the hash of the empty string in the persona-hash field (consistent with AC 03-01 Edge Case 1), since callers are expected to validate `Record` shape upstream before reaching this constructor

## Performance Requirements
- **Response Time:** Constructing one `TelemetryPersonaRecord` completes in well under 1ms (one `HashPersonaID` call plus field copies).
- **Throughput:** No batching requirement in this story; the type is a plain data struct with negligible allocation cost per call.

## Security Considerations
- **Authentication/Authorization:** N/A — in-process data transformation only, no network or storage I/O in this AC.
- **Input Validation:** The constructor accepts any `Record` value without rejecting malformed input (mirrors `AnonymizeRecord`'s permissive style); it must never copy `Record.Reviewer` (or any other raw identity field) into the resulting `TelemetryPersonaRecord` in unhashed form.
- **Data Minimization:** The schema's field set is a deliberate allowlist of its own — only fields required for Persona Leaderboard aggregation are present; no `run_id`, cost, or token fields are carried over.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Sample `Record` values with varying `Reviewer`/`Model` combinations, including a zero-value `Record{}`.
**Mock/Stub Requirements:** None — pure in-memory struct construction, no external dependencies.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `TelemetryPersonaRecord` (or equivalently named type) is defined in `internal/scorecard/telemetry.go`, distinct from `PublicRecord`
- [ ] Constructor populates the persona-hash field via `HashPersonaID` and never copies the raw `Reviewer` value unhashed
- [ ] JSON-marshaled output contains no raw Persona ID, `Reviewer`, or `run_id` value
- [ ] Zero-value `Record{}` input does not panic

**Manual Review:**
- [ ] Code reviewed and approved

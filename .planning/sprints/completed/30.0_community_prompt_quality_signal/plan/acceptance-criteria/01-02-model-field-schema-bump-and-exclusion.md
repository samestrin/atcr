# Acceptance Criteria: `Model` Field Schema Bump and Attribution-Incomplete Exclusion

**Related User Story:** [01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters](../user-stories/01-aggregate-per-persona-model-dismissal-counters.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct field addition + write-path wiring | `internal/localdebt.Record`, `cmd/atcr/reconcile.go` `persistLocalDebt` |
| Test Framework | Go `testing` (table-driven) | Mirrors `internal/localdebt/record_test.go` and `internal/localdebt/store_test.go` schema-compat coverage |
| Key Dependencies | `encoding/json` (stdlib) | Backward-compatible JSONL read/write, no new dependency |

## Related Files
- `internal/localdebt/record.go` - modify: bump `SchemaVersion` 1 → 2 and add `Model string` (`json:"model,omitempty"`) to `Record`, documenting the v1→v2 forward/backward-compat contract on the constant per existing convention
- `cmd/atcr/reconcile.go` - modify: `persistLocalDebt` (around line 271-286) populates the new `rec.Model` field from `fanout.AgentStatus.Model` (already in scope at that write site) when constructing each `localdebt.Record`
- `internal/localdebt/qualitysignal.go` - modify (depends on AC 01-01): `AggregateQualitySignal` excludes any record with an empty/unresolved `Model` from per-model rows rather than grouping it under an empty-string model bucket
- `internal/localdebt/record_test.go` - modify: add coverage asserting a v1 (no `Model`) record still round-trips through `ReadAll` without error
- `internal/localdebt/qualitysignal_test.go` - modify (depends on AC 01-01): add fixture records with `SchemaVersion: 1` (no model) mixed with `SchemaVersion: 2` (with model) records

### Related Files (from codebase-discovery.json)

- `internal/localdebt/record.go` - update: bump `SchemaVersion` 1 → 2 (`:9`) and add `Model string` (`json:"model,omitempty"`) to `Record` (`:24-44`)
- `cmd/atcr/reconcile.go` - update: `persistLocalDebt` (`:228`) populates `rec.Model` at the record-construction site (`:271-286`) from the `fanout.AgentStatus.Model` data already in scope there (read path: `internal/fanout/artifacts.go:180` `ReadPoolSummary`)
- `internal/localdebt/qualitysignal.go` - update (AC 01-01): exclude empty-`Model` records from per-model rows
- `internal/localdebt/record_test.go` - update: schema v1→v2 round-trip coverage
- `internal/localdebt/qualitysignal_test.go` - update (AC 01-01): mixed v1/v2 fixtures

## Happy Path Scenarios
**Scenario 1: Schema v2 record carries Model end-to-end**
- **Given** a fan-out run whose `AgentStatus.Model` is `"claude-sonnet-4-6"` for persona `security-reviewer`
- **When** `persistLocalDebt` writes the resulting finding to `.atcr/debt/`
- **Then** the persisted JSONL record has `"schema_version":2` and `"model":"claude-sonnet-4-6"`, and a subsequent `AggregateQualitySignal` call includes that record in the `("security-reviewer", "claude-sonnet-4-6")` group

**Scenario 2: Mixed v1/v2 store aggregates correctly**
- **Given** a `.atcr/debt/` store containing both pre-existing schema-v1 records (no `Model`) and new schema-v2 records (with `Model`)
- **When** `localdebt.ReadAll` then `AggregateQualitySignal` run over the combined set
- **Then** v2 records are grouped and counted per `(persona, model)` as normal, and v1 records are excluded from all per-model rows without causing a read error or corrupting any v2 group's counts

## Edge Cases
**Edge Case 1: Schema v1 record (no Model field present in JSON)**
- **Given** a JSONL line with `"schema_version":1` and no `"model"` key at all
- **When** `localdebt.ReadAll` parses it
- **Then** the record decodes successfully with `Model == ""` (Go zero value), is read without error or warning, and `AggregateQualitySignal` excludes it from every per-model row (it is not bucketed under an empty-string model)

**Edge Case 2: Schema v2 record with an empty Model value**
- **Given** a schema-v2 record where `Model` was written as `""` (e.g. a write-site regression, or a fan-out run where model attribution genuinely could not be determined)
- **When** `AggregateQualitySignal` processes it
- **Then** it is excluded from per-model rows identically to a v1 record — "attribution-incomplete" is defined by an empty `Model` value regardless of declared schema version, not by the version number alone

**Edge Case 3: Forward-compat — a hypothetical schema v3 record**
- **Given** a record with `"schema_version":3` (newer than this code understands)
- **When** `localdebt.ReadAll` parses it
- **Then** the existing forward-incompatible-skip behavior documented on `SchemaVersion` continues to apply unchanged — this AC does not alter that contract, only adds a new field understood at v2

## Error Conditions
**Error Scenario 1: Malformed JSON line**
- **Given** a JSONL line that fails to unmarshal
- **Then** existing behavior is unchanged: the line is skipped and `localdebt.MsgMalformedSkip` ("skipping malformed record") is logged to the diagnostic writer — this AC introduces no new error path

## Performance Requirements
- **Response Time:** Adding one `omitempty` string field to `Record` and one field assignment in `persistLocalDebt` has no measurable effect on read/write throughput (single-digit-byte increase per record).
- **Throughput:** No change to `ReadAll`'s existing O(n) linear-scan read characteristics.

## Security Considerations
- **Authentication/Authorization:** Not applicable — local file write, same trust boundary as existing `persistLocalDebt` writes.
- **Input Validation:** `Model` is copied verbatim from `fanout.AgentStatus.Model` (already-validated internal catalog slug per `internal/scorecard/telemetry.go`'s documented non-PII rationale) — no new external input is introduced, and the field carries no code/path/finding-content.

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION (read/write round-trip)
**Test Data Requirements:** Fixture JSONL lines for schema v1 (no model key), schema v2 with a populated model, and schema v2 with an empty model string; a `fanout.AgentStatus` fixture with a non-empty `Model` for the `persistLocalDebt` write-path test.
**Mock/Stub Requirements:** None beyond existing `localdebt.ReadAll`/`Append` — no external service involved; use `t.TempDir()` for the store directory as `store_test.go` already does.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/localdebt/... ./cmd/atcr/...`)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `Record.SchemaVersion` constant is 2 and `Model` field exists with `omitempty`
- [x] `persistLocalDebt` populates `Model` from `fanout.AgentStatus.Model` at write time
- [x] `AggregateQualitySignal` excludes any record with an empty `Model` (v1 or v2) from per-model rows
- [x] A v1 record with no `model` key round-trips through `ReadAll` without error

**Manual Review:**
- [ ] Code reviewed and approved

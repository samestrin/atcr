# Acceptance Criteria: Metric Preservation & Metadata Integrity

**Related User Story:** [04: Export Public Leaderboard Submission](../user-stories/04-export-public-leaderboard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Package | `internal/scorecard` | `export.go` — `AnonymizeRecord` field mapping |
| Data Model | Go structs | `PublicRecord` with all numeric metric fields |
| Serialization | `encoding/json` (stdlib) | JSON number precision (float64 default) |
| Test Framework | `go test` + `testify/assert` | Exact numeric comparison, field presence checks |

### Related Files

- `internal/scorecard/export.go` - create: field mapping from `ScorecardRecord` to `PublicRecord` preserving all metrics
- `internal/scorecard/scorecard.go` - reference: source `ScorecardRecord` field definitions and types
- `internal/scorecard/export_test.go` - create: tests asserting exact numeric equality and field completeness

## Happy Path Scenarios

**Scenario 1: All numeric metrics preserved with exact values**
- **Given** a `ScorecardRecord` with `findings_raised: 120`, `findings_corroborated: 78`, `findings_solo: 42`, `corroboration_rate: 0.65`, `findings_verified: 50`, `findings_refuted: 8`, `survived_skeptic_rate: 0.86`, `cost_usd: 0.60`, `tokens_in: 213000`, `tokens_out: 60000`, `latency_ms_avg: 9100`
- **When** `AnonymizeRecord` is called
- **Then** the resulting `PublicRecord` contains all 11 numeric fields with exactly the same values; no rounding, truncation, or type conversion alters the values

**Scenario 2: Model identifier preserved as-is**
- **Given** a `ScorecardRecord` with `model: "claude-sonnet-4-6"`
- **When** `AnonymizeRecord` is called
- **Then** the `PublicRecord` has `model: "claude-sonnet-4-6"` (exact string match, no transformation)

**Scenario 3: Reviewer persona name preserved as-is**
- **Given** a `ScorecardRecord` with `reviewer: "bruce"`
- **When** `AnonymizeRecord` is called
- **Then** the `PublicRecord` has `reviewer: "bruce"` (persona names are not considered PII)

**Scenario 4: Role field preserved as-is**
- **Given** a `ScorecardRecord` with `role: "reviewer"`
- **When** `AnonymizeRecord` is called
- **Then** the `PublicRecord` has `role: "reviewer"`

**Scenario 5: schema_version is always 1**
- **Given** any valid export operation
- **When** the export JSON is generated
- **Then** the top-level `schema_version` field is integer `1` (not string, not float)

**Scenario 6: Exported JSON contains all required v1 schema fields**
- **Given** a valid export with at least one record
- **When** the JSON is parsed
- **Then** each record contains all 14 required fields: `index`, `reviewer`, `model`, `role`, `runs`, `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `findings_verified`, `findings_refuted`, `survived_skeptic_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms_avg`

**Scenario 7: Records with verification data include verification metrics**
- **Given** a record aggregated from runs that had `verification.json` present (so `findings_verified`, `findings_refuted`, `survived_skeptic_rate` are populated)
- **When** `AnonymizeRecord` is called
- **Then** the verification metrics are preserved with their exact values in the public record

**Scenario 8: Records without verification data emit zero for verification metrics**
- **Given** a record aggregated from runs that had no `verification.json` (so `findings_verified: 0`, `findings_refuted: 0`, `survived_skeptic_rate: 0.0`)
- **When** `AnonymizeRecord` is called
- **Then** the verification metric fields are present with zero values (not omitted/null)

## Edge Cases

**Edge Case 1: Zero-value metrics are preserved (not omitted)**
- **Given** a record where `findings_raised: 0`, `cost_usd: 0.0`, `tokens_in: 0`
- **When** `AnonymizeRecord` is called
- **Then** these fields appear in the JSON output with value `0` or `0.0` (not omitted via `omitempty`)

**Edge Case 2: Floating-point precision for rates**
- **Given** a record with `corroboration_rate: 0.3333333333333333`
- **When** `AnonymizeRecord` is called and the result is marshaled to JSON
- **Then** the JSON output preserves at least 2 decimal places of precision for rate fields; the value is not rounded to 0 or 1

**Edge Case 3: Large token counts (integer overflow check)**
- **Given** a record with `tokens_in: 100000000` (100M tokens aggregated across many runs)
- **When** `AnonymizeRecord` is called and marshaled
- **Then** the token count is serialized as a JSON integer, not scientific notation

## Error Conditions

**Error Scenario 1: Type mismatch in source record**
- **Given** a hypothetical future where a numeric field in `ScorecardRecord` is changed to a string type
- **When** `AnonymizeRecord` is called
- **Then** the Go compiler catches the type mismatch at build time (struct field assignment is type-checked); this is a compile-time error, not a runtime error

## Performance Requirements
- **Throughput:** Field copying for 10,000 records completes in < 100ms (trivial struct copy)
- **Precision:** No precision loss in float64 → JSON serialization for rates and costs

## Security Considerations
- **No metric inflation:** Anonymization must not alter metric values. A corrupted metric would mislead the public leaderboard. Exact field copy (not transform) is the correct approach.
- **Zero-value inclusion:** Using `omitempty` on numeric fields would leak information (absence implies zero, which is still data). All numeric fields must always be present.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- `ScorecardRecord` fixtures with known metric values (integers, floats, edge cases)
- Expected `PublicRecord` structs with identical metric values
- Records with and without verification data

**Mock/Stub Requirements:**
- No mocks needed; `AnonymizeRecord` is a pure function
- Use `assert.Equal` for exact numeric comparison
- Marshal `PublicRecord` to JSON and unmarshal back to verify field presence and types (catches `omitempty` bugs)
- Test: parse exported JSON, assert all 14+ required fields exist per record using reflection or explicit checks

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./internal/scorecard/...` passes, including metric preservation tests
- [ ] `go vet ./internal/scorecard/...` clean
- [ ] Test assertion: every required field in v1 schema is present in marshaled JSON (no `omitempty` on numeric fields)
- [ ] Test assertion: numeric values in JSON match source values exactly

**Story-Specific:**
- [ ] All 11 numeric metric fields are preserved: `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `findings_verified`, `findings_refuted`, `survived_skeptic_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms_avg`
- [ ] `model`, `reviewer`, `role` are preserved as-is (not anonymized)
- [ ] `schema_version` is integer `1`
- [ ] Zero-value metrics are included in output (not omitted)
- [ ] `runs` field (count of aggregated runs per reviewer) is included

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Field types in `PublicRecord` match v1 schema spec (integers for counts, floats for rates/costs)
- [ ] No `omitempty` tags on any fields in `PublicRecord` struct

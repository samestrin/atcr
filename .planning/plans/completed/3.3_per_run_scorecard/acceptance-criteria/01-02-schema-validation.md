# Acceptance Criteria: Versioned Schema Record Shape

**Related User Story:** [01: Auto-emit Scorecard](../user-stories/01-auto-emit-scorecard.md)

## Acceptance Criteria Statement
Every scorecard record conforms to schema version `1` and includes all required fields derived from reconcile output, matching the record shape defined in `original-requirements.md`.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Record Schema | Go struct with JSON tags | `schema_version: 1` constant |
| Validation | `encoding/json` + struct field checks | Required-field enforcement |
| Test Framework | `go test` + `testify` | Table-driven tests |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/scorecard.go` — create: Record struct definition and builder
- `internal/scorecard/store.go` — modify: marshal record to JSON line with `schema_version`
- `internal/reconcile/reconcile.go:55` — reference: `Reconcile` pipeline producing `Result{Findings, Summary}`
- `internal/reconcile/merge.go:40` — reference: `Merged` struct with `Reviewers []string` for attribution/corroboration
- `internal/registry/config.go:81` — reference: `AgentConfig` maps reviewer name to `Model` and `Role`

## Happy Path Scenarios
**Scenario 1: Per-reviewer record contains all required fields**
- **Given** a reconcile run with reviewer "reviewer-A" using model "claude-sonnet-4-20250514"
- **When** scorecard record is built
- **Then** JSON record contains: `schema_version`, `run_id`, `reviewer`, `model`, `role`, `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms`

**Scenario 2: schema_version is always 1**
- **Given** any reconcile run
- **When** record is serialized
- **Then** `schema_version` field equals `1` (integer, not string)

**Scenario 3: All numeric fields are populated from reconcile data**
- **Given** reconcile Result with known finding counts, token usage, cost, and latency
- **When** record is built
- **Then** `findings_raised`, `corroboration_rate`, `cost_usd`, `tokens_in`, `tokens_out`, `latency_ms` match source data exactly

## Edge Cases
**Edge Case 1: Zero findings raised**
- **Given** reviewer raised 0 findings
- **When** record is built
- **Then** `findings_raised: 0`, `corroboration_rate: 0.0` (not null/omitted)

**Edge Case 2: Missing role in AgentConfig**
- **Given** reviewer has no explicit role set
- **When** record is built
- **Then** `role` field defaults to empty string `""` (not null)

**Edge Case 3: Corroboration rate division by zero**
- **Given** reviewer raised 0 findings
- **When** corroboration_rate is computed
- **Then** rate is `0.0` (not NaN or Infinity)

## Error Conditions
**Error Scenario 1: Missing required reviewer identity**
- Error message: `scorecard: record missing required field: reviewer`
- Record is not written; warning logged

## Performance Requirements
- **Response Time:** Record construction < 1ms per reviewer
- **Throughput:** N+1 records built synchronously before single file write

## Security Considerations
- **Input Validation:** All string fields validated for JSON safety; no raw user input injected without escaping
- **Data Protection:** `cost_usd` and token counts are operational data; stored locally only

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture reconcile Results with known values; edge case fixtures (zero findings, missing role)
**Mock/Stub Requirements:** None — pure struct construction

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Record struct has all 14+ required fields with JSON tags
- [ ] `schema_version` is integer constant `1`
- [ ] Corroboration rate handles zero-division safely
- [ ] Round-trip test: build record → marshal → unmarshal → assert field equality

**Manual Review:**
- [ ] Code reviewed and approved

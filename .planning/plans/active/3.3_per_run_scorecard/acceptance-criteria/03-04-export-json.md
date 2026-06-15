# Acceptance Criteria: Export Versioned JSON

**Related User Story:** [03: View Aggregated Leaderboard](../user-stories/03-view-aggregated-leaderboard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Flag | `cobra` flag `--export` (bool) | When true, output JSON instead of table |
| Export Builder | `internal/scorecard/export.go` | `ExportJSON(rows []LeaderboardRow, meta ExportMeta) ([]byte, error)` |
| JSON Serialization | `encoding/json` with `json.MarshalIndent` | Human-readable, versioned output |
| Schema Version | Constant `SchemaVersion = 1` in `internal/scorecard/scorecard.go` | Pinned to v1 |
| Test Framework | `go test` + `testify/assert` | Schema validation tests |

### Related Files

- `cmd/atcr/leaderboard.go` - modify: add `--export` flag, branch on flag to output JSON vs table
- `internal/scorecard/export.go` - create: `ExportJSON()` function, `ExportEnvelope` struct
- `internal/scorecard/aggregate.go` - reference: `LeaderboardRow` struct as input to export
- `internal/scorecard/scorecard.go` - reference: `SchemaVersion` constant

## Happy Path Scenarios

**Scenario 1: Basic export**
- **Given** the leaderboard has 3 aggregated rows from filtered records
- **When** `atcr leaderboard --export` is executed
- **Then** the output is valid JSON with: `schema_version: 1`, `generated_at` (ISO timestamp), `filters` object (applied filter values), and `entries` array containing the 3 aggregated rows

**Scenario 2: Export respects filters**
- **Given** the store contains records for multiple models and reviewers
- **When** `atcr leaderboard --model claude-sonnet-4-6 --since 7d --export` is executed
- **Then** the exported JSON `entries` array contains only records matching the filters, and the `filters` object reflects `model: claude-sonnet-4-6`, `since: 7d`

**Scenario 3: Export is anonymized**
- **Given** the leaderboard data contains reviewer names, model names, and metrics
- **When** `atcr leaderboard --export` is executed
- **Then** the JSON output contains no provider API keys, no repository content, no file paths, no PII — only reviewer/persona identifiers, model names, and aggregate metrics

**Scenario 4: Export JSON is valid and parseable**
- **Given** any valid leaderboard state
- **When** `atcr leaderboard --export` is executed
- **Then** the output passes `json.Unmarshal` without error and matches the expected schema structure

**Scenario 5: Export envelope includes metadata**
- **Given** a leaderboard generated with `--since 30d`
- **When** `atcr leaderboard --export` is executed
- **Then** the JSON envelope contains:
  - `schema_version`: 1
  - `generated_at`: ISO 8601 timestamp
  - `date_range`: { `from`: oldest record timestamp, `to`: newest record timestamp }
  - `filters`: { `since`: "30d", `model`: "", `persona`: "" }
  - `entries`: array of aggregated records

## Edge Cases

**Edge Case 1: Export with no matching records**
- **Given** filters result in zero matching records
- **When** `atcr leaderboard --since 1d --export` is executed
- **Then** the JSON output has an empty `entries` array, metadata reflects the filters applied, and exit code is 0

**Edge Case 2: Export with single record**
- **Given** only one reviewer record exists
- **When** `atcr leaderboard --export` is executed
- **Then** the JSON `entries` array has exactly one element with correct field values

**Edge Case 3: Export does not include aggregate records**
- **Given** the store contains per-reviewer records and aggregate records
- **When** `atcr leaderboard --export` is executed
- **Then** the `entries` array contains only per-reviewer aggregated data; no `role: aggregate` entries

## Error Conditions

**Error Scenario 1: Export to unwritable destination (future: --output flag)**
- **Given** `--export` writes to stdout by default
- **When** stdout is redirected to a read-only file descriptor
- **Then** the command exits with code 1 and prints a write error to stderr

## Performance Requirements
- **Serialization:** JSON marshaling of up to 10,000 aggregated rows completes within 500ms
- **Memory:** Export builds the JSON in memory; for 10,000 rows at ~200 bytes each, peak memory is ~2MB

## Security Considerations
- **Anonymization:** Export MUST NOT contain: provider API keys, repository paths, file contents, personal identifiers beyond reviewer persona names
- **Schema Pinning:** `schema_version` field is set to `1`; future format changes increment the version
- **Output Destination:** Writes to stdout only; user controls file redirection. No automatic file creation.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Aggregated leaderboard rows with known values
- Edge case: empty entries, single entry, multiple entries

**Mock/Stub Requirements:** None — pure serialization

**Test Pattern:**
```go
func TestExportJSON(t *testing.T) {
    rows := []aggregate.LeaderboardRow{
        {Reviewer: "bruce", Model: "claude-sonnet-4-6", Runs: 3, Corroborated: 12, CorrRate: 0.52, TotalCost: 0.05},
    }
    meta := export.ExportMeta{Since: "30d", Model: "", Persona: ""}

    data, err := export.ExportJSON(rows, meta)
    require.NoError(t, err)

    var envelope export.ExportEnvelope
    require.NoError(t, json.Unmarshal(data, &envelope))
    assert.Equal(t, 1, envelope.SchemaVersion)
    assert.Len(t, envelope.Entries, 1)
    assert.Equal(t, "bruce", envelope.Entries[0].Reviewer)
}

func TestExportAnonymization(t *testing.T) {
    // Verify no API keys, file paths, or repo content in output
    // Check that output contains only expected fields
}
```

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./internal/scorecard/...` passes
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go vet ./...` clean
- [ ] `go build ./...` succeeds
- [ ] Test coverage >= 90% on `export.go`
- [ ] JSON output passes schema validation in tests

**Story-Specific:**
- [ ] `--export` flag produces valid JSON output to stdout
- [ ] JSON includes `schema_version: 1`
- [ ] JSON includes `generated_at`, `date_range`, `filters` metadata
- [ ] JSON `entries` array contains aggregated leaderboard rows
- [ ] Export is anonymized: no provider keys, repo paths, file content, or PII
- [ ] Export respects all active filters (`--since`, `--model`, `--persona`)
- [ ] Aggregate records (role=`aggregate`) are excluded from export

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Export schema reviewed against Epic 10.0 public leaderboard submission requirements
- [ ] JSON output manually inspected for correctness and completeness

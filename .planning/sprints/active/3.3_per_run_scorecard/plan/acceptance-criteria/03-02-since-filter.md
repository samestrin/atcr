# Acceptance Criteria: Time Range Filter (--since)

**Related User Story:** [03: View Aggregated Leaderboard](../user-stories/03-view-aggregated-leaderboard.md)

## Acceptance Criteria Statement
The `--since` flag filters scorecard records to those within the specified time window before aggregation, defaulting to the last 30 days.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Flag | `cobra` flag `--since` (string) | Default: `30d` |
| Duration Parser | Custom parser in `internal/scorecard/aggregate.go` | Supports `Nd`, `Nw`, `Nm` formats |
| Timestamp Source | `run_id` field (ISO timestamp prefix) | Compare record timestamp against `time.Now().Add(-duration)` |
| Test Framework | `go test` + `testify/assert` | Table-driven tests for duration parsing and filtering |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/leaderboard.go` — modify: add `--since` flag with default `30d`
- `internal/scorecard/aggregate.go` — modify: `ParseDuration(s string) (time.Duration, error)` function; time filter applied before aggregation
- `internal/scorecard/store.go` — reference: `Record` struct with `RunID` field
- `internal/scorecard/scorecard.go` — reference: `run_id` generation format (ISO timestamp prefix)

## Happy Path Scenarios

**Scenario 1: Default since filter (30 days)**
- **Given** the store contains records from 10 days ago and 45 days ago
- **When** `atcr leaderboard` is executed (no `--since` flag)
- **Then** only the record from 10 days ago is included in the leaderboard aggregation

**Scenario 2: Explicit 7-day filter**
- **Given** the store contains records from 3 days ago, 6 days ago, and 15 days ago
- **When** `atcr leaderboard --since 7d` is executed
- **Then** only records from 3 and 6 days ago are included

**Scenario 3: Week-based filter**
- **Given** the store contains records spanning 3 weeks
- **When** `atcr leaderboard --since 2w` is executed
- **Then** only records from the last 14 days are included

**Scenario 4: Month-based filter**
- **Given** the store contains records spanning 3 months
- **When** `atcr leaderboard --since 2m` is executed
- **Then** only records from the last 60 days are included

**Scenario 5: 90-day filter includes all recent records**
- **Given** the store contains records from 10, 30, 60, and 89 days ago
- **When** `atcr leaderboard --since 90d` is executed
- **Then** all four records are included in the aggregation

## Edge Cases

**Edge Case 1: No records within the time window**
- **Given** all records in the store are older than the `--since` window
- **When** `atcr leaderboard --since 7d` is executed
- **Then** the command prints `No records match filters. Try widening --since or removing filters.` and exits with code 0

**Edge Case 2: All records fall within the time window**
- **Given** all records are from the last 2 days
- **When** `atcr leaderboard --since 30d` is executed
- **Then** all records are included; output is identical to running without `--since`

**Edge Case 3: Boundary timestamp (exactly at cutoff)**
- **Given** a record with timestamp exactly at the cutoff boundary (e.g., exactly 7 days ago for `--since 7d`)
- **When** `atcr leaderboard --since 7d` is executed
- **Then** the record is included (inclusive boundary: `>=` cutoff)

## Error Conditions

**Error Scenario 1: Invalid duration format**
- **Given** the user passes an unrecognized format
- **When** `atcr leaderboard --since abc` is executed
- **Then** the command exits with code 1 and prints: `Error: invalid --since value 'abc'. Supported formats: Nd (days), Nw (weeks), Nm (months). Example: 30d, 2w, 3m`

**Error Scenario 2: Zero or negative duration**
- **Given** the user passes `--since 0d` or `--since -1d`
- **When** `atcr leaderboard --since 0d` is executed
- **Then** the command exits with code 1 and prints: `Error: --since must be a positive duration`

**Error Scenario 3: Empty duration value**
- **Given** the user passes `--since` with no value
- **When** `atcr leaderboard --since` is executed
- **Then** cobra returns an error: `Error: flag needs an argument: --since`

## Performance Requirements
- **Filter Speed:** Time filtering is O(n) — single pass over records, comparing timestamps
- **No Impact on Baseline:** Adding `--since` does not increase memory usage beyond the record already being processed

## Security Considerations
- **Input Validation:** Duration string is parsed with strict format validation; no injection risk
- **No External Input:** Timestamps come from local JSONL records, not user-provided at query time

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Records with controlled timestamps: 1d, 5d, 10d, 30d, 60d, 90d ago
- Records at exact boundary timestamps

**Mock/Stub Requirements:**
- Time is injectable for deterministic tests (use `time.Now()` with a fixed reference or inject a clock)

**Test Pattern:**
```go
func TestParseSinceDuration(t *testing.T) {
    tests := []struct {
        input   string
        want    time.Duration
        wantErr bool
    }{
        {"7d", 7 * 24 * time.Hour, false},
        {"2w", 14 * 24 * time.Hour, false},
        {"3m", 90 * 24 * time.Hour, false},
        {"abc", 0, true},
        {"0d", 0, true},
        {"-1d", 0, true},
    }
    // ...
}

func TestAggregateWithSinceFilter(t *testing.T) {
    // Create records with fixed timestamps relative to a test "now"
    // Apply filter; assert correct records included/excluded
}
```

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/scorecard/...` passes
- [x] `go test ./cmd/atcr/...` passes
- [x] `go vet ./...` clean
- [x] `go build ./...` succeeds
- [x] Test coverage >= 90% on `ParseSinceDuration` and time filter logic

**Story-Specific:**
- [x] `--since` flag is defined with default value `30d`
- [x] Supports `Nd`, `Nw`, `Nm` duration formats
- [x] Invalid formats produce clear error messages with usage hints
- [x] Time filter is applied before aggregation; only matching records contribute to leaderboard
- [x] Default behavior (no `--since`) uses 30-day window
- [x] Empty result set after filtering prints informative message and exits 0

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Duration parsing handles all documented formats
- [ ] Error messages are actionable and match the format in the error scenarios

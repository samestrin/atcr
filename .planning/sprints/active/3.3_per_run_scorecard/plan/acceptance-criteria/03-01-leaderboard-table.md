# Acceptance Criteria: Leaderboard Ranked Table Display

**Related User Story:** [03: View Aggregated Leaderboard](../user-stories/03-view-aggregated-leaderboard.md)

## Acceptance Criteria Statement
The `atcr leaderboard` command reads all stored scorecard records, aggregates them by `(reviewer, model)`, and renders a ranked table sorted by corroboration rate descending.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Command | `cobra.Command` in `cmd/atcr/leaderboard.go` | New subcommand registered in `cmd/atcr/main.go` |
| Aggregation | `internal/scorecard/aggregate.go` | `Aggregate(records []Record) []LeaderboardRow` |
| Table Rendering | `text/tabwriter` | Consistent with `internal/doctor/render.go` pattern |
| JSONL Reader | `internal/scorecard/store.go` | Shared with Story 1/2; reads all `*.jsonl` from `~/.config/atcr/scorecard/` |
| Test Framework | `go test` + `testify/assert` | Table-driven tests |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/leaderboard.go` — create: `leaderboard` cobra command, flag definitions, table output
- `internal/scorecard/aggregate.go` — create: `Aggregate()` function, `LeaderboardRow` struct, ranking logic
- `internal/scorecard/store.go` — modify: ensure `ReadAll()` or equivalent returns records from all monthly JSONL files
- `internal/doctor/render.go:45` — reference: `RenderTable` tabwriter initialization and column alignment pattern
- `cmd/atcr/main.go` — modify: register `leaderboard` command
- `cmd/atcr/scorecard.go` — reference: table rendering pattern for per-reviewer output (Story 2)

## Happy Path Scenarios

**Scenario 1: Display leaderboard with default sorting**
- **Given** the scorecard store contains records from 3 runs with reviewers `bruce` (model: `claude-sonnet-4-6`), `diana` (model: `gpt-4o`), and `bruce` appearing in all 3 runs
- **When** `atcr leaderboard` is executed
- **Then** the output is a table with columns: reviewer, model, runs, findings raised, findings corroborated, corroboration rate, total cost, cost per corroborated finding, avg latency — sorted by corroboration rate descending

**Scenario 2: Aggregation groups by (reviewer, model) key**
- **Given** the store contains records: `bruce`/`claude-sonnet-4-6` (runs 1,2,3) and `bruce`/`gpt-4o` (run 1) — same reviewer name, different models
- **When** `atcr leaderboard` is executed
- **Then** the table shows two separate rows for `bruce`: one for `claude-sonnet-4-6` (runs=3) and one for `gpt-4o` (runs=1), each with independent metrics

**Scenario 3: Corroboration rate computed as total corroborated / total raised**
- **Given** `bruce`/`claude-sonnet-4-6` has 3 runs: run1 (raised=10, corroborated=6), run2 (raised=5, corroborated=4), run3 (raised=8, corroborated=2)
- **When** `atcr leaderboard` is executed
- **Then** the row for `bruce`/`claude-sonnet-4-6` shows: runs=3, findings raised=23, findings corroborated=12, corroboration rate=0.52 (12/23)

**Scenario 4: Cost per corroborated finding computed as total cost / total corroborated**
- **Given** `diana`/`gpt-4o` has total cost $0.1500 and 5 corroborated findings across all runs
- **When** `atcr leaderboard` is executed
- **Then** the row shows: total cost=$0.1500, cost per corroborated finding=$0.0300 ($0.1500/5)

**Scenario 5: Cost per corroborated finding when zero corroborated**
- **Given** `eve`/`haiku` has total cost $0.0200 and 0 corroborated findings
- **When** `atcr leaderboard` is executed
- **Then** the row shows cost per corroborated finding as `—` (dash, not $0.00 or NaN)

**Scenario 6: Table fits standard terminal width**
- **Given** leaderboard data with up to 10 reviewer/model combinations
- **When** `atcr leaderboard` is executed
- **Then** the table output fits within 120 columns and uses aligned columns via `text/tabwriter`

## Edge Cases

**Edge Case 1: Single record in store**
- **Given** the store contains exactly one reviewer record from one run
- **When** `atcr leaderboard` is executed
- **Then** the table shows one data row with correct values; runs=1

**Edge Case 2: All reviewers have zero corroboration rate**
- **Given** all records have `findings_corroborated=0`
- **When** `atcr leaderboard` is executed
- **Then** the table is displayed with all corroboration rates at 0.00; sorted consistently (secondary sort by reviewer name or total cost)

**Edge Case 3: Aggregate records skipped**
- **Given** the store contains per-reviewer records and aggregate records (role=`aggregate`)
- **When** `atcr leaderboard` is executed
- **Then** aggregate records are excluded from the leaderboard table; only per-reviewer records are aggregated

## Error Conditions

**Error Scenario 1: Scorecard directory does not exist**
- **Given** `~/.config/atcr/scorecard/` does not exist (no reconcile runs have occurred since Epic 3.3)
- **When** `atcr leaderboard` is executed
- **Then** the command prints `No scorecard data found. Run 'atcr reconcile' to generate scorecard records.` and exits with code 0

**Error Scenario 2: All JSONL files are empty**
- **Given** the scorecard directory exists but all `.jsonl` files are zero bytes
- **When** `atcr leaderboard` is executed
- **Then** the command prints an informative message and exits with code 0

## Performance Requirements
- **Response Time:** Command completes within 2 seconds for up to 10,000 stored records
- **Memory:** Stream-parse JSONL files line-by-line; do not load entire files into memory before processing
- **Throughput:** Aggregation (group-by + sum/avg computation) must be O(n) where n is the number of records

## Security Considerations
- **Authentication/Authorization:** N/A — all data is local, no network calls
- **Input Validation:** No user input beyond CLI flags; JSONL records are parsed with strict schema validation; malformed lines are skipped with warnings
- **Data Integrity:** Read-only operation; no mutation of the scorecard store

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Fixtures: multiple JSONL files with known records (various reviewer/model combos, corroboration rates, costs)
- Edge case fixtures: empty files, single-record files, files with aggregate records mixed in

**Mock/Stub Requirements:**
- Scorecard store reader can be injected or use a temp directory with fixture JSONL files

**Test Pattern:**
```go
func TestLeaderboardTable(t *testing.T) {
    tests := []struct {
        name       string
        records    []scorecard.Record
        wantRows   []expectedRow // reviewer, model, runs, corrRate, costPerCorr
        wantSorted bool          // verify descending corroboration rate order
    }{
        // table cases covering happy paths and edge cases...
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            rows := aggregate.Aggregate(tt.records)
            // assert row count, values, sort order
        })
    }
}
```

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/scorecard/...` passes
- [x] `go test ./cmd/atcr/...` passes
- [x] `go vet ./...` clean
- [x] `go build ./...` succeeds
- [x] Test coverage >= 90% on `aggregate.go`

**Story-Specific:**
- [x] `atcr leaderboard` displays a table with all required columns: reviewer, model, runs, findings raised, findings corroborated, corroboration rate, total cost, cost per corroborated finding, avg latency
- [x] Table is sorted by corroboration rate descending
- [x] Records are grouped by (reviewer, model) key
- [x] Aggregate records (role=`aggregate`) are excluded
- [x] Cost per corroborated finding shows `—` when zero corroborated
- [x] Table renders via `text/tabwriter` with aligned columns

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Table output visually verified in a terminal at 120-column width
- [ ] Output is consistent with `atcr scorecard` table formatting (Story 2)

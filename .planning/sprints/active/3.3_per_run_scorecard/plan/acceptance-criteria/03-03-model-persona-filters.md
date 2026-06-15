# Acceptance Criteria: Model and Persona Filters

**Related User Story:** [03: View Aggregated Leaderboard](../user-stories/03-view-aggregated-leaderboard.md)

## Acceptance Criteria Statement
The `--model` and `--persona` flags filter scorecard records by exact model and reviewer matches, composing with AND semantics and with the `--since` filter.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Flags | `cobra` flags `--model` (string) and `--persona` (string) | Both optional, no defaults |
| Filter Logic | `internal/scorecard/aggregate.go` | `FilterByModel(records, model)` and `FilterByPersona(records, persona)` |
| Composition | Sequential filtering in `cmd/atcr/leaderboard.go` | All filters applied before aggregation |
| Test Framework | `go test` + `testify/assert` | Table-driven tests for each filter and combinations |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/leaderboard.go` — modify: add `--model` and `--persona` flags, compose filters
- `internal/scorecard/aggregate.go` — modify: filter functions for model and persona
- `internal/scorecard/store.go` — reference: `Record` struct fields `Model` and `Reviewer`
- `internal/scorecard/scorecard.go` — reference: record schema — `model` and `reviewer` field names

## Happy Path Scenarios

**Scenario 1: Filter by model**
- **Given** the store contains records for models `claude-sonnet-4-6`, `gpt-4o`, and `haiku`
- **When** `atcr leaderboard --model claude-sonnet-4-6` is executed
- **Then** only records with `model=claude-sonnet-4-6` are included in the leaderboard aggregation

**Scenario 2: Filter by persona**
- **Given** the store contains records for reviewers `bruce`, `diana`, and `eve`
- **When** `atcr leaderboard --persona bruce` is executed
- **Then** only records with `reviewer=bruce` are included in the leaderboard aggregation

**Scenario 3: Composable filters — all three applied**
- **Given** the store contains records spanning multiple reviewers, models, and dates
- **When** `atcr leaderboard --since 7d --model claude-sonnet-4-6 --persona bruce` is executed
- **Then** only records matching all three criteria are included: within last 7 days AND model=`claude-sonnet-4-6` AND reviewer=`bruce`

**Scenario 4: Filters are AND semantics**
- **Given** the store contains: record A (reviewer=bruce, model=claude-sonnet-4-6), record B (reviewer=bruce, model=gpt-4o), record C (reviewer=diana, model=claude-sonnet-4-6)
- **When** `atcr leaderboard --model claude-sonnet-4-6 --persona bruce` is executed
- **Then** only record A is included (both conditions must match)

**Scenario 5: No model filter shows all models**
- **Given** the store contains records for 3 different models
- **When** `atcr leaderboard` is executed (no `--model` flag)
- **Then** records from all models are included in the aggregation

**Scenario 6: No persona filter shows all reviewers**
- **Given** the store contains records for 3 different reviewers
- **When** `atcr leaderboard` is executed (no `--persona` flag)
- **Then** records from all reviewers are included in the aggregation

## Edge Cases

**Edge Case 1: Model filter matches no records**
- **Given** the store contains records only for `gpt-4o`
- **When** `atcr leaderboard --model nonexistent-model` is executed
- **Then** the command prints `No records match filters. Try widening --since or removing filters.` and exits with code 0

**Edge Case 2: Persona filter matches no records**
- **Given** the store contains records for reviewers `bruce` and `diana`
- **When** `atcr leaderboard --persona eve` is executed
- **Then** the command prints `No records match filters. Try widening --since or removing filters.` and exits with code 0

**Edge Case 3: Combined filters yield no results when individual filters would**
- **Given** records exist for reviewer=bruce with model=gpt-4o and reviewer=diana with model=claude-sonnet-4-6
- **When** `atcr leaderboard --model claude-sonnet-4-6 --persona bruce` is executed
- **Then** no records match (bruce doesn't use claude-sonnet-4-6); informative message displayed

**Edge Case 4: Model name with special characters**
- **Given** a record with model name containing dots and dashes (e.g., `claude-sonnet-4-6`)
- **When** `atcr leaderboard --model claude-sonnet-4-6` is executed
- **Then** exact string match is performed; the record is found

## Error Conditions

**Error Scenario 1: Empty model value**
- **Given** the user passes `--model ""` (empty string)
- **When** `atcr leaderboard --model ""` is executed
- **Then** the command treats it as no filter (all models shown), or exits with code 1 and error: `Error: --model must not be empty`

**Error Scenario 2: Empty persona value**
- **Given** the user passes `--persona ""` (empty string)
- **When** `atcr leaderboard --persona ""` is executed
- **Then** the command treats it as no filter (all reviewers shown), or exits with code 1 and error: `Error: --persona must not be empty`

## Performance Requirements
- **Filter Speed:** Each filter is O(n) — single pass over records; three filters composed sequentially remain O(n)
- **No Amplification:** Filtering does not allocate copies of the record slice; operates on the same slice with predicates

## Security Considerations
- **Input Validation:** Filter values are plain strings compared with `==`; no injection risk
- **No External Input:** Values are matched against locally stored record fields

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Records with varying models: `claude-sonnet-4-6`, `gpt-4o`, `haiku`
- Records with varying reviewers: `bruce`, `diana`, `eve`
- Combinations of model + reviewer + timestamps for filter composition tests

**Mock/Stub Requirements:** None — pure in-memory filtering

**Test Pattern:**
```go
func TestLeaderboardFilters(t *testing.T) {
    tests := []struct {
        name      string
        records   []scorecard.Record
        model     string
        persona   string
        wantCount int
        wantKeys  []string // reviewer/model combos expected
    }{
        {
            name:    "model filter",
            records: mixedRecords(),
            model:   "claude-sonnet-4-6",
            wantCount: 2,
        },
        {
            name:    "persona filter",
            records: mixedRecords(),
            persona: "bruce",
            wantCount: 3,
        },
        {
            name:    "combined filters",
            records: mixedRecords(),
            model:   "claude-sonnet-4-6",
            persona: "bruce",
            wantCount: 1,
        },
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            filtered := applyFilters(tt.records, tt.model, tt.persona)
            assert.Len(t, filtered, tt.wantCount)
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
- [x] Test coverage >= 90% on filter functions

**Story-Specific:**
- [x] `--model <name>` filters records to exact model match
- [x] `--persona <name>` filters records to exact reviewer match
- [x] Filters compose with AND semantics — all specified filters must match
- [x] Filters compose with `--since` — time filter and identity filters work together
- [x] Omitted filters default to "match all" (no restriction)
- [x] Empty result after filtering prints informative message and exits 0

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Flag names are consistent with existing CLI conventions (no aliases needed)
- [ ] Help text for each flag is clear and includes examples

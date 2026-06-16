# Acceptance Criteria: Determinism, Filtering & Error Handling

**Related User Story:** [04: Export Public Leaderboard Submission](../user-stories/04-export-public-leaderboard.md)

## Acceptance Criteria Statement
Export output is deterministic for identical inputs and filters, records are sorted by `(model, reviewer, role)` ascending, and empty or unmatched result sets exit with code 1.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Package | `internal/scorecard` | `export.go` — sort, filter, serialize pipeline |
| CLI | Cobra (`github.com/spf13/cobra`) | Filter flags on `leaderboardCmd` |
| Sorting | `sort.Slice` (stdlib) | Deterministic sort by `(model, reviewer, role)` |
| Test Framework | `go test` + `testify/assert` | Byte-identical comparison, filter combination tests |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/export.go` — create: deterministic sort + filter pipeline in `Export()`
- `internal/scorecard/aggregate.go` — reference: filter application (`--since`, `--model`, `--persona`)
- `cmd/atcr/leaderboard.go` — modify: wire `--export` flag to `Export()` with filter options
- `internal/scorecard/export_test.go` — create: determinism tests (double-run byte comparison), filter tests
- `internal/scorecard/store.go` — reference: JSONL reader for empty-store detection

## Happy Path Scenarios

**Scenario 1: Export is deterministic — byte-identical across runs**
- **Given** a scorecard store with multiple records across different models and reviewers
- **When** `atcr leaderboard --export` is executed twice with the same filters
- **Then** the two outputs are byte-identical (verified via `sha256sum` comparison or `assert.Equal` on byte slices)

**Scenario 2: Records sorted by (model, reviewer, role) before serialization**
- **Given** records for models `["gpt-4", "claude-sonnet-4-6"]`, reviewers `["alice", "bruce"]`, roles `["reviewer", "skeptic"]`
- **When** `atcr leaderboard --export` is executed
- **Then** records appear in the output JSON sorted first by `model` (ascending), then `reviewer` (ascending), then `role` (ascending); `claude-sonnet-4-6/alice/reviewer` appears before `claude-sonnet-4-6/bruce/reviewer`, which appears before `gpt-4/alice/reviewer`

**Scenario 3: --since filter applied before anonymization**
- **Given** records spanning the last 90 days and `--since 30d` is specified
- **When** `atcr leaderboard --export --since 30d` is executed
- **Then** only records from the last 30 days are included in the export; the `filters.since` field in the output JSON is `"30d"`

**Scenario 4: --model filter applied before anonymization**
- **Given** records for models `claude-sonnet-4-6` and `gpt-4`
- **When** `atcr leaderboard --export --model claude-sonnet-4-6` is executed
- **Then** only records for `claude-sonnet-4-6` appear in the output; the `filters.model` field is `"claude-sonnet-4-6"`

**Scenario 5: --persona filter applied before anonymization**
- **Given** records for reviewers `bruce` and `alice`
- **When** `atcr leaderboard --export --persona bruce` is executed
- **Then** only records for reviewer `bruce` appear; the `filters.persona` field is `"bruce"`

**Scenario 6: JSON output uses 2-space indent for readability**
- **Given** a valid export with at least one record
- **When** the JSON is inspected
- **Then** the output is formatted with 2-space indentation (from `json.MarshalIndent` with `"  "` indent string), not minified

**Scenario 7: exported_at timestamp is RFC 3339 format**
- **Given** a valid export operation
- **When** the JSON is generated
- **Then** the `exported_at` field is a valid RFC 3339 timestamp (e.g., `"2026-06-15T10:00:00Z"`) in UTC

## Edge Cases

**Edge Case 1: Single record export**
- **Given** the scorecard store contains exactly one record matching the filters
- **When** `atcr leaderboard --export` is executed
- **Then** the output JSON contains `records` array with exactly one element; `index` is `0`

**Edge Case 2: Filters produce empty result set**
- **Given** the scorecard store has records but none match `--model nonexistent-model`
- **When** `atcr leaderboard --export --model nonexistent-model` is executed
- **Then** exit code is 1; message is: `"No records match the specified filters. Try widening --since or removing filters."`; no JSON is written to stdout or file

**Edge Case 3: Scorecard store is completely empty (no JSONL files)**
- **Given** `~/.config/atcr/scorecard/` directory exists but contains no `.jsonl` files
- **When** `atcr leaderboard --export` is executed
- **Then** exit code is 1; message is: `"No records match the specified filters. Try widening --since or removing filters."`

**Edge Case 4: Scorecard store directory does not exist**
- **Given** `~/.config/atcr/scorecard/` does not exist (no reconcile runs have occurred)
- **When** `atcr leaderboard --export` is executed
- **Then** exit code is 1; message is: `"No records match the specified filters. Try widening --since or removing filters."` (same message as empty store — no implementation details leaked)

**Edge Case 5: Determinism with identical filter values in different order**
- **Given** flags `--since 30d --model claude-sonnet-4-6` are specified
- **When** `atcr leaderboard --export --model claude-sonnet-4-6 --since 30d` is executed (flags in different order)
- **Then** the output is byte-identical to the first ordering (flag order does not affect output)

## Error Conditions

**Error Scenario 1: Corrupt JSONL file in scorecard store**
- **Given** a `.jsonl` file in the scorecard store contains malformed JSON lines
- **When** `atcr leaderboard --export` is executed
- **Then** malformed lines are skipped with a warning to stderr (consistent with `atcr leaderboard` table view)
- **And** the export continues using valid records
- **And** if no valid records remain, the command exits with code 1 and prints: `"No records match the specified filters. Try widening --since or removing filters."`

**Error Scenario 2: Scorecard store directory is not readable**
- **Given** `~/.config/atcr/scorecard/` exists but has permissions `000`
- **When** `atcr leaderboard --export` is executed
- **Then** the command exits with code 1 and prints a permissions-related error message

## Performance Requirements
- **Sorting:** Sort of 10,000 records by `(model, reviewer, role)` completes in < 100ms
- **Determinism overhead:** Deterministic serialization adds < 50ms over non-deterministic (sort cost dominates)
- **Filter application:** Filter evaluation is O(n) over records; < 200ms for 10,000 records

## Security Considerations
- **No information leakage via error messages:** Error messages do not reveal internal paths, file names, or system details beyond what the user already knows
- **Deterministic output prevents timing attacks:** No per-record randomization or timestamps that could be used to correlate exports with specific runs
- **`exported_at` is metadata only:** The export timestamp is in the top-level metadata, not in individual records; it does not affect reproducibility of the record data itself

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Multiple `ScorecardRecord` fixtures with varying models, reviewers, roles, and dates
- Fixture sets designed to test sort order (records in shuffled order, verify sorted output)

**Mock/Stub Requirements:**
- Stub scorecard store to return fixture records
- For determinism test: call `Export()` twice with identical input, assert `bytes.Equal(output1, output2)`
- For filter tests: create records with known dates/models/personas, apply each filter, verify subset
- For empty store tests: stub store returning zero records, assert exit code 1 behavior

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/scorecard/... ./cmd/atcr/...` passes
- [x] `go vet ./internal/scorecard/... ./cmd/atcr/...` clean
- [x] Determinism test: two consecutive `Export()` calls produce byte-identical output
- [x] Sort test: output records are in `(model, reviewer, role)` ascending order

**Story-Specific:**
- [x] Records sorted by `(model, reviewer, role)` before serialization
- [x] `--since`, `--model`, `--persona` filters applied before anonymization
- [x] `filters` object in output JSON reflects active filter values
- [x] `exported_at` is RFC 3339 UTC timestamp
- [x] JSON formatted with 2-space indent (`json.MarshalIndent`)
- [x] No matching records → exit code 1 with message: `"No records match the specified filters. Try widening --since or removing filters."`
- [x] Empty store / missing store directory → same exit code 1 message (no internal path leakage)

**Manual Review:**
- [x] Code reviewed and approved
- [x] Sort order is stable and documented in code comments
- [x] Error messages reviewed for information leakage

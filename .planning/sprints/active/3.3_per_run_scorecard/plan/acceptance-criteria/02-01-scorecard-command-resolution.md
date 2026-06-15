# Acceptance Criteria: Scorecard Command Resolution and Lookup

**Related User Story:** [02: View Single-Run Scorecard](../user-stories/02-view-single-run-scorecard.md)

## Acceptance Criteria Statement
Running `atcr scorecard [id-or-path]` resolves the argument to a `run_id`, reads the matching scorecard records from the monthly JSONL store, and renders a per-reviewer table.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Go CLI Command | `cobra` + `cmd/atcr/scorecard.go` | New file; registers `atcr scorecard [id-or-path]` |
| Argument Resolution | `os.Stat` + path heuristics | Mirror `cmd/atcr/anchor.go` pattern (path vs id discrimination) |
| JSONL Store Reader | `internal/scorecard/store.go` (Story 1) | `ReadRecords(runID string)` or equivalent query function |
| Month Derivation | `time.Parse` on ISO timestamp prefix | `run_id` contains `2026-06-14T...` → derive `2026-06` for file lookup |
| Test Framework | `go test` + `testify/assert` | Table-driven tests for argument resolution |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/scorecard.go` — create: cobra command definition, argument parsing, RunE handler
- `cmd/atcr/main.go` — modify: register `newScorecardCmd()` in root command tree
- `internal/scorecard/store.go` — create (Story 1) or modify: add `ReadRecords(runID string) ([]Record, error)` query function
- `cmd/atcr/anchor.go:45` — reference: `resolveReviewDir` shared review-directory resolution pattern
- `internal/fanout/reviewdir.go:42` — reference: `ReviewID` derivation for `run_id`

## Happy Path Scenarios

**Scenario 1: Lookup by run_id**
- **Given** the JSONL store at `~/.config/atcr/scorecard/2026-06.jsonl` contains 3 records with `run_id` = `"2026-06-14T10:00:00Z-abc123"` for reviewers Alice, Bob, and Carol
- **When** the user runs `atcr scorecard 2026-06-14T10:00:00Z-abc123`
- **Then** the command reads `2026-06.jsonl`, filters to the 3 matching records, and renders a table with 3 data rows
- **And** exits with code 0

**Scenario 2: Lookup by directory path (relative)**
- **Given** the reconcile output directory at `./reconciled/` contains a `summary.json` with `run_id` = `"2026-06-14T10:00:00Z-abc123"`
- **And** the JSONL store contains matching records for that `run_id`
- **When** the user runs `atcr scorecard ./reconciled/`
- **Then** the command extracts the `run_id` from the directory's `summary.json`
- **And** queries the JSONL store for matching records
- **And** renders the table

**Scenario 3: Lookup by directory path (absolute)**
- **Given** the reconcile output directory at `/tmp/reviews/2026-06-14_abc123/` contains a valid `summary.json`
- **When** the user runs `atcr scorecard /tmp/reviews/2026-06-14_abc123/`
- **Then** the command reads `run_id` from the `summary.json` at that path
- **And** renders the matching scorecard table

**Scenario 4: Month derivation from run_id**
- **Given** a `run_id` = `"2026-06-14T10:00:00Z-abc123"` (timestamp prefix `2026-06`)
- **When** the command resolves which JSONL file to read
- **Then** it reads only `~/.config/atcr/scorecard/2026-06.jsonl`
- **And** does not scan other monthly files

## Edge Cases

**Edge Case 1: run_id spans month boundary (record stored in adjacent month file)**
- **Given** a `run_id` with timestamp `2026-06-30T23:59:59Z` but the record was written to `2026-07.jsonl` (clock skew or late write)
- **When** the command queries based on the derived month `2026-06`
- **Then** the command falls back to scanning adjacent month files (`2026-05`, `2026-07`) if the primary month file yields no matches
- **And** logs a warning about the fallback

**Edge Case 2: Argument is a path without summary.json**
- **Given** the path `./output/` exists as a directory but contains no `reconciled/summary.json`
- **When** the user runs `atcr scorecard ./output/`
- **Then** the command treats the argument as a `run_id` instead
- **And** if no records match, exits with the "no data" error (AC 02-03)

**Edge Case 3: Multiple records per reviewer for the same run_id**
- **Given** the JSONL store contains 2 records with the same `run_id` and `reviewer` = `"Alice"` (duplicate emit from a retry)
- **When** the command renders the table
- **Then** it uses the last record for that reviewer (last-write-wins)
- **And** the table shows exactly 1 row for Alice

## Error Conditions

**Error Scenario 1: No argument provided**
- **Given** the user runs `atcr scorecard` with no arguments
- **Then** the command prints usage information
- **And** exits with code 2 (usage error)
- Error message: `"requires a run_id or path argument"`

**Error Scenario 2: Invalid run_id format (path traversal attempt)**
- **Given** the user runs `atcr scorecard ../../../etc/passwd`
- **When** the argument does not look like a path (no separators after validation) but fails `ValidateReviewID`
- **Then** the command exits with code 2
- Error message: `"invalid review id: must not contain path separators or '..'"`

**Error Scenario 3: Scorecard store directory does not exist**
- **Given** `~/.config/atcr/scorecard/` does not exist (Story 1 has never run)
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the command exits with code 1
- Error message: `"no scorecard records found for run <run_id>: run 'atcr reconcile' to generate data"`

## Performance Requirements
- **Response Time:** Command completes in <500ms for a JSONL file with up to 1000 records
- **File I/O:** Reads only the relevant monthly JSONL file (derived from `run_id` timestamp), not a full directory scan
- **Memory:** Streams JSONL line-by-line; does not load entire file into memory when not needed

## Security Considerations
- **Input Validation:** run_id validated via `fanout.ValidateReviewID` — rejects path separators, `..`, leading dashes
- **Path Argument:** Explicit paths are used verbatim (matching `anchorDir` pattern); no path traversal beyond what the user provides
- **JSONL Parsing:** Line-length limit (e.g., 1MB per line) prevents memory exhaustion from corrupted files
- **No Mutation:** Read-only command; never writes to the scorecard store or reconcile output

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Temporary JSONL files with known records (3 reviewers, one run_id)
- Temporary directory structures with `summary.json` containing `run_id`
- Edge case: JSONL with duplicate reviewer entries
- Edge case: empty JSONL file
- Edge case: JSONL with unparseable lines

**Mock/Stub Requirements:**
- `os.UserConfigDir()` or `~/.config` override for test isolation (env var or injectable store path)
- Filesystem operations via `afero` or temp directories (no real `~/.config` writes)

**Test Cases:**
1. `TestScorecardCmd_LookupByRunID` — happy path, records found
2. `TestScorecardCmd_LookupByRelativePath` — path resolution via summary.json
3. `TestScorecardCmd_LookupByAbsolutePath` — absolute path resolution
4. `TestScorecardCmd_MonthDerivation` — correct file selected from run_id timestamp
5. `TestScorecardCmd_DuplicateReviewer` — last-write-wins deduplication
6. `TestScorecardCmd_NoArgs` — usage error, exit 2
7. `TestScorecardCmd_InvalidRunID` — path traversal rejected, exit 2
8. `TestScorecardCmd_StoreNotFound` — missing directory, clear error

## Definition of Done

**Auto-Verified:**
- [x] All unit tests passing (`go test ./cmd/atcr/... ./internal/scorecard/...`)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `atcr scorecard <run_id>` resolves records from correct monthly JSONL file
- [x] `atcr scorecard <path>` extracts `run_id` from `summary.json` and renders results
- [x] Argument discrimination (path vs id) matches `anchorDir` pattern
- [x] run_id validated with `ValidateReviewID` — no path traversal possible

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Error messages are actionable and match documented strings

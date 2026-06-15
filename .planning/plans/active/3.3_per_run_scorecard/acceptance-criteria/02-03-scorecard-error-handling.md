# Acceptance Criteria: Scorecard Error Handling and Edge Case Resilience

**Related User Story:** [02: View Single-Run Scorecard](../user-stories/02-view-single-run-scorecard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Error Handling | Go `error` + `fmt.Errorf` wrapping | Coded errors via `codedError` pattern from `main.go` |
| JSONL Parser | `bufio.Scanner` + `json.Unmarshal` | Line-by-line with skip-on-error semantics |
| Exit Codes | `codedError{code: exitUsage}` (exit 2) / plain error (exit 1) | Consistent with existing CLI error taxonomy |
| Test Framework | `go test` + `testify/assert` | Error assertion + exit code verification |
| Key Dependencies | `os` for file stat, `io/fs` for directory walking |

## Related Files
- `cmd/atcr/scorecard.go` - modify: error paths for missing data, corrupted files, invalid arguments
- `internal/scorecard/store.go` - modify: `ReadRecords` returns distinguishable errors (not found vs corrupt)
- `cmd/atcr/scorecard_test.go` - create: error scenario tests with exit code assertions
- `cmd/atcr/main.go` - reference: `codedError`, `usageError`, `exitCode` pattern

## Happy Path Scenarios

**Scenario 1: Graceful handling of unparseable JSONL lines**
- **Given** a JSONL file with 5 valid records and 2 lines that are not valid JSON (e.g., truncated writes, partial records)
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the command skips the 2 unparseable lines with a warning to stderr
- **And** renders the table from the 5 valid records
- **And** exits with code 0
- Warning format: `"atcr: warning: skipping malformed scorecard record at line N: <reason>"`

**Scenario 2: Graceful handling of oversized JSONL lines**
- **Given** a JSONL file where one line exceeds the 1MB line-length limit
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the oversized line is skipped with a warning
- **And** remaining valid records are rendered normally
- Warning format: `"atcr: warning: skipping oversized scorecard record at line N (>1MB)"`

**Scenario 3: Empty scorecard store directory**
- **Given** `~/.config/atcr/scorecard/` exists but is empty (no monthly JSONL files)
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the command exits with code 1
- Error message: `"no scorecard records found for run <run_id>: run 'atcr reconcile' to generate data"`

## Edge Cases

**Edge Case 1: JSONL file exists but contains no records for the given run_id**
- **Given** `~/.config/atcr/scorecard/2026-06.jsonl` exists with records for `run_id` = `"2026-06-13T08:00:00Z-xyz789"`
- **When** the user runs `atcr scorecard 2026-06-14T10:00:00Z-abc123`
- **Then** the command finds no matching records after filtering
- **And** exits with code 1
- Error message: `"no scorecard records found for run 2026-06-14T10:00:00Z-abc123: run 'atcr reconcile' to generate data"`

**Edge Case 2: Run completed before Epic 3.3 (no scorecard emission)**
- **Given** a reconcile output directory from before scorecard emission existed
- **And** the `run_id` from its `summary.json` has no matching JSONL records
- **When** the user runs `atcr scorecard ./old-reconciled/`
- **Then** the command exits with code 1
- Error message: `"no scorecard records found for run <run_id>: reconcile was run before scorecard emission was enabled"`

**Edge Case 3: JSONL file with mixed valid/invalid JSON (partial corruption)**
- **Given** a JSONL file where the first 3 lines are valid, line 4 is truncated JSON (`{"run_id":"2026-`), and lines 5-7 are valid
- **When** the user runs `atcr scorecard <run_id>`
- **Then** lines 1-3 and 5-7 are processed normally
- **And** line 4 triggers a stderr warning
- **And** the table renders from the 6 valid records

**Edge Case 4: Scorecard store path is a file, not a directory**
- **Given** `~/.config/atcr/scorecard` exists as a regular file (not a directory)
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the command exits with code 1
- Error message: `"scorecard store is not a directory: ~/.config/atcr/scorecard"`

## Error Conditions

**Error Scenario 1: Path argument points to non-existent directory**
- **Given** the user runs `atcr scorecard /nonexistent/path/`
- **When** `/nonexistent/path/` does not exist on the filesystem
- **Then** the command exits with code 2 (usage error)
- Error message: `"no sources found in /nonexistent/path/: run 'atcr review' first"`

**Error Scenario 2: summary.json in path is unreadable (permissions)**
- **Given** the user runs `atcr scorecard ./restricted-dir/`
- **And** `./restricted-dir/reconciled/summary.json` exists but has mode `0o000`
- **When** the command attempts to read the file
- **Then** the command exits with code 1
- Error message: `"failed to read summary.json in ./restricted-dir/: permission denied"`

**Error Scenario 3: summary.json contains invalid JSON**
- **Given** the user runs `atcr scorecard ./corrupt-review/`
- **And** `./corrupt-review/reconciled/summary.json` contains `{invalid json`
- **When** the command attempts to parse the file
- **Then** the command exits with code 1
- Error message: `"failed to parse summary.json in ./corrupt-review/: invalid character 'i' looking for beginning of object key string"`

**Error Scenario 4: JSONL file is unreadable (permissions)**
- **Given** `~/.config/atcr/scorecard/2026-06.jsonl` exists with mode `0o000`
- **When** the command attempts to read the file
- **Then** the command exits with code 1
- Error message: `"failed to read scorecard store: permission denied"`

**Error Scenario 5: JSONL record missing required `run_id` field**
- **Given** a JSONL line with valid JSON but no `run_id` field: `{"reviewer":"Alice","model":"gemini-2.5-pro"}`
- **When** the command processes the line
- **Then** the record is skipped with a warning
- Warning format: `"atcr: warning: skipping scorecard record at line N: missing run_id"`

## Performance Requirements
- **Error Recovery:** Unparseable lines are skipped in <1ms each; total overhead negligible
- **Scanner Buffer:** `bufio.Scanner` buffer sized at 1MB (`bufio.MaxScanTokenSize` or custom) to handle oversized lines without panic
- **No Partial Output:** When an error causes non-zero exit, no table output is written to stdout (error goes to stderr)

## Security Considerations
- **Input Validation:** All path arguments validated before filesystem access; `ValidateReviewID` for bare ids
- **File Permissions:** Respect OS file permissions; do not attempt to override or bypass
- **Error Messages:** Do not leak full filesystem paths in error messages beyond what the user provided (e.g., if the user says `./foo`, errors reference `./foo`, not the resolved absolute path)
- **Line-Length Limit:** Prevents memory exhaustion from malformed JSONL files (1MB per-line cap)
- **No Eval/Exec:** No dynamic code execution from JSONL content; pure `json.Unmarshal` parsing

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- JSONL files with mixed valid/invalid lines (golden data)
- JSONL files with oversized lines (>1MB)
- Corrupt `summary.json` files
- Temporary directories with various permission modes
- JSONL records missing required fields

**Mock/Stub Requirements:**
- Filesystem via temp directories (no real `~/.config` writes)
- `os.Chmod` for permission edge cases (skip on Windows)
- `bytes.Buffer` for capturing both stdout and stderr separately

**Test Cases:**
1. `TestScorecardCmd_SkipMalformedLines` — valid records rendered, malformed lines warned
2. `TestScorecardCmd_SkipOversizedLines` — >1MB lines skipped with warning
3. `TestScorecardCmd_NoRecordsFound` — exit 1 with clear message when run_id not in store
4. `TestScorecardCmd_EmptyStore` — exit 1 when store directory is empty
5. `TestScorecardCmd_StoreNotDirectory` — exit 1 when store path is a file
6. `TestScorecardCmd_NonexistentPath` — exit 2 for invalid directory argument
7. `TestScorecardCmd_UnreadableSummary` — exit 1 for permission-denied summary.json
8. `TestScorecardCmd_CorruptSummary` — exit 1 with parse error message
9. `TestScorecardCmd_MissingRunID` — record without run_id skipped with warning
10. `TestScorecardCmd_NoPartialOutputOnError` — stdout empty when command fails

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests passing (`go test ./cmd/atcr/... ./internal/scorecard/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)
- [ ] `go vet` passes with no warnings

**Story-Specific:**
- [ ] Unparseable JSONL lines skipped with stderr warning (not hard failure)
- [ ] No scorecard records → exit 1 with message: `"no scorecard records found for run <id>: run 'atcr reconcile' to generate data"`
- [ ] Invalid arguments → exit 2 (usage error)
- [ ] On error, no partial table written to stdout
- [ ] Line-length limit prevents memory exhaustion from malformed JSONL
- [ ] Error messages do not leak internal paths beyond user input

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Error messages tested manually in terminal for clarity and actionability
- [ ] Verified behavior when `~/.config/atcr/scorecard/` does not exist (fresh install)

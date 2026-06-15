# Acceptance Criteria: Graceful Empty and Missing Data Handling

**Related User Story:** [03: View Aggregated Leaderboard](../user-stories/03-view-aggregated-leaderboard.md)

## Acceptance Criteria Statement
The `atcr leaderboard` command handles missing scorecard directory, empty files, malformed JSONL lines, and unsupported schema versions with informative messages and exit code 0 (or 1 for true errors).

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Directory Check | `os.Stat` + `os.IsNotExist` | Detect missing scorecard directory |
| File Scanner | `bufio.Scanner` line-by-line | Stream-parse JSONL; skip malformed lines |
| Error Logging | `fmt.Fprintf(os.Stderr, ...)` | Warnings for skipped lines, info messages for empty state |
| Exit Code | `os.Exit(0)` for graceful empty | Non-zero only for actual errors (invalid flags) |
| Test Framework | `go test` + `testify/assert` | Temp directory fixtures for missing/empty scenarios |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/leaderboard.go` — modify: check for missing/empty store before aggregation, print informative messages
- `internal/scorecard/store.go` — modify: `ReadAll()` returns empty slice (not error) when directory is missing; skips malformed lines with warning
- `internal/scorecard/scorecard.go` — reference: expected record schema for validation during read

## Happy Path Scenarios

**Scenario 1: Missing scorecard directory**
- **Given** `~/.config/atcr/scorecard/` does not exist (no reconcile runs have occurred)
- **When** `atcr leaderboard` is executed
- **Then** the command prints `No scorecard data found. Run 'atcr reconcile' to generate scorecard records.` to stdout and exits with code 0

**Scenario 2: Empty scorecard directory**
- **Given** `~/.config/atcr/scorecard/` exists but contains no `.jsonl` files
- **When** `atcr leaderboard` is executed
- **Then** the command prints `No scorecard data found. Run 'atcr reconcile' to generate scorecard records.` and exits with code 0

**Scenario 3: JSONL files exist but are all empty**
- **Given** `~/.config/atcr/scorecard/` contains `2026-06.jsonl` (0 bytes)
- **When** `atcr leaderboard` is executed
- **Then** the command prints the informative empty-data message and exits with code 0

**Scenario 4: JSONL files contain only malformed lines**
- **Given** `2026-06.jsonl` contains 3 lines of invalid JSON
- **When** `atcr leaderboard` is executed
- **Then** warnings are printed to stderr for each malformed line (e.g., `Warning: skipping malformed record in 2026-06.jsonl:3`), and the command prints the empty-data message and exits with code 0

**Scenario 5: Mix of valid and malformed records**
- **Given** `2026-06.jsonl` contains 5 valid records and 2 malformed lines
- **When** `atcr leaderboard` is executed
- **Then** the leaderboard is rendered using the 5 valid records; warnings are printed to stderr for the 2 malformed lines; exit code is 0

**Scenario 6: Non-JSONL files in scorecard directory**
- **Given** `~/.config/atcr/scorecard/` contains `2026-06.jsonl` (valid) and `notes.txt` (non-JSONL file)
- **When** `atcr leaderboard` is executed
- **Then** only `*.jsonl` files are read; `notes.txt` is ignored; leaderboard renders normally

## Edge Cases

**Edge Case 1: Scorecard directory is a file, not a directory**
- **Given** `~/.config/atcr/scorecard` exists but is a regular file (not a directory)
- **When** `atcr leaderboard` is executed
- **Then** the command prints `Error: scorecard path is not a directory: ~/.config/atcr/scorecard` and exits with code 1

**Edge Case 2: JSONL file with trailing newline (empty last line)**
- **Given** `2026-06.jsonl` ends with a blank line (common when appending)
- **When** `atcr leaderboard` is executed
- **Then** the empty line is silently skipped (not treated as a malformed record); valid records are processed normally

**Edge Case 3: Records with different schema_version**
- **Given** the store contains records with `schema_version: 1` and `schema_version: 2` (future)
- **When** `atcr leaderboard` is executed
- **Then** records with unrecognized `schema_version` are skipped with a warning (`Warning: skipping record with unsupported schema_version 2`); v1 records are processed normally

**Edge Case 4: Permission denied on scorecard directory**
- **Given** `~/.config/atcr/scorecard/` exists but is not readable (permissions)
- **When** `atcr leaderboard` is executed
- **Then** the command prints `Error: cannot read scorecard directory: permission denied` and exits with code 1

## Error Conditions

**Error Scenario 1: Corrupted JSONL file (partial write)**
- **Given** a JSONL file was partially written during a crash — last line is truncated JSON
- **When** `atcr leaderboard` is executed
- **Then** the truncated line is skipped with a warning; all complete records are processed; exit code is 0

**Error Scenario 2: Home directory not resolvable**
- **Given** `$HOME` is not set and `os.UserHomeDir()` fails
- **When** `atcr leaderboard` is executed
- **Then** the command prints `Error: cannot determine home directory for scorecard path` and exits with code 1

## Performance Requirements
- **Graceful Degradation:** Malformed lines do not halt processing; remaining valid lines continue to be read
- **Stream Parsing:** Large JSONL files are read line-by-line via `bufio.Scanner`; memory usage is O(1) per line, not O(n) for the full file
- **Warning Throttling:** If more than 100 malformed lines exist in a single file, print a summary warning (`Warning: 150 malformed lines skipped in 2026-06.jsonl`) instead of one per line

## Security Considerations
- **Path Safety:** Scorecard directory path is derived from `os.UserHomeDir()` + fixed suffix; no user-provided path injection
- **Malformed Data:** JSON parsing errors are caught per-line; no panic or crash from malformed input
- **Permissions:** File I/O respects OS-level permissions; no privilege escalation

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Temp directories simulating: missing directory, empty directory, empty JSONL files, mixed valid/malformed JSONL, non-JSONL files
- Malformed JSON fixtures: truncated lines, invalid JSON, wrong types

**Mock/Stub Requirements:**
- Override scorecard directory path for testing (via env var, function parameter, or test helper)

**Test Pattern:**
```go
func TestLeaderboardGracefulHandling(t *testing.T) {
    tests := []struct {
        name       string
        setupDir   func(t *testing.T) string // returns dir path
        wantExit   int
        wantOutput string // substring expected in stdout
        wantWarn   string // substring expected in stderr
    }{
        {
            name:     "missing directory",
            setupDir: func(t *testing.T) string { return "/nonexistent/path" },
            wantExit: 0,
            wantOutput: "No scorecard data found",
        },
        {
            name: "empty directory",
            setupDir: func(t *testing.T) string {
                return t.TempDir()
            },
            wantExit: 0,
            wantOutput: "No scorecard data found",
        },
        {
            name: "malformed lines mixed with valid",
            setupDir: func(t *testing.T) string {
                dir := t.TempDir()
                // write file with mix of valid/invalid JSON lines
                return dir
            },
            wantExit: 0,
            wantWarn: "skipping malformed record",
        },
    }
    // execute leaderboard logic with each setup, assert output and exit code
}
```

## Definition of Done

**Auto-Verified:**
- [x] `go test ./internal/scorecard/...` passes
- [x] `go test ./cmd/atcr/...` passes
- [x] `go vet ./...` clean
- [x] `go build ./...` succeeds
- [x] Test coverage >= 90% on error handling paths in `store.go` and `leaderboard.go`

**Story-Specific:**
- [x] Missing scorecard directory prints informative message and exits 0
- [x] Empty scorecard directory prints informative message and exits 0
- [x] Empty JSONL files are handled gracefully (no crash, informative message)
- [x] Malformed JSONL lines are skipped with per-line warnings to stderr
- [x] Non-JSONL files in the directory are ignored (only `*.jsonl` read)
- [x] Blank trailing lines in JSONL files are silently skipped
- [x] Unsupported `schema_version` records are skipped with warning
- [x] Partial/truncated writes from crashes are handled without crashing

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Error messages are clear and actionable for the end user
- [ ] Warning format is consistent with other atcr commands

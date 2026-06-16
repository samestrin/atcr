# Acceptance Criteria: Export Command & Public Submission Schema

**Related User Story:** [04: Export Public Leaderboard Submission](../user-stories/04-export-public-leaderboard.md)

## Acceptance Criteria Statement
The `atcr leaderboard --export` command produces a versioned JSON document conforming to the v1 public submission schema, writing to stdout by default or to a file via `--output`.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| CLI Framework | Cobra (`github.com/spf13/cobra`) | Extends `leaderboardCmd` with `--export` flag |
| Package | `cmd/atcr`, `internal/scorecard` | New `export.go` in both packages |
| Serialization | `encoding/json` (stdlib) | `MarshalIndent` with 2-space indent |
| Test Framework | `go test` + `testify/assert` | Table-driven tests, golden-file comparison |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/leaderboard.go` — modify: add `--export` and `--output` flags to existing leaderboard command
- `internal/scorecard/export.go` — create: `Export(records, filters) ([]byte, error)` and `WriteExport(data, outputPath) error`
- `internal/scorecard/aggregate.go` — reference: reuse aggregation pipeline from Story 3
- `internal/scorecard/store.go` — reference: JSONL reader for loading scorecard records
- `cmd/atcr/leaderboard_test.go` — create: tests for `--export` flag parsing, output routing, exit codes

## Happy Path Scenarios

**Scenario 1: Export produces valid v1 schema JSON to stdout**
- **Given** the scorecard store contains aggregated records from prior reconcile runs
- **When** `atcr leaderboard --export` is executed
- **Then** a single JSON document is written to stdout conforming to the v1 public submission schema: `{ "schema_version": 1, "exported_at": "<RFC3339>", "filters": { ... }, "records": [ ... ] }`, and the process exits with code 0

**Scenario 2: Export writes to file via --output flag**
- **Given** the scorecard store contains records
- **When** `atcr leaderboard --export --output /tmp/submission.json` is executed
- **Then** the JSON document is written to `/tmp/submission.json` (not stdout), the file is valid JSON conforming to v1 schema, and the process exits with code 0

**Scenario 3: Export with --output creates parent directories**
- **Given** the scorecard store contains records and `/tmp/nested/` does not exist
- **When** `atcr leaderboard --export --output /tmp/nested/deep/submission.json` is executed
- **Then** parent directories are created, the file is written successfully, and the process exits with code 0

**Scenario 4: Export help output includes --export and --output flags**
- **Given** the `atcr` binary is built
- **When** `atcr leaderboard --help` is executed
- **Then** usage text lists `--export` (emit anonymized public submission JSON) and `--output <path>` (write to file instead of stdout)

## Edge Cases

**Edge Case 1: Export with --output to existing file overwrites**
- **Given** `/tmp/existing.json` already exists with different content
- **When** `atcr leaderboard --export --output /tmp/existing.json` is executed
- **Then** the file is atomically overwritten (write to temp file then rename) with the new export; the old content is fully replaced

**Edge Case 2: Export to stdout can be piped**
- **Given** the scorecard store contains records
- **When** `atcr leaderboard --export | jq .schema_version` is executed
- **Then** stdout receives only the JSON document (no extra log lines, no prompts), and `jq` parses `schema_version` as `1`

**Edge Case 3: Export with all filter flags combined**
- **Given** records exist for multiple models, personas, and date ranges
- **When** `atcr leaderboard --export --since 7d --model claude-sonnet-4-6 --persona bruce` is executed
- **Then** the exported JSON `filters` object reflects `{ "since": "7d", "model": "claude-sonnet-4-6", "persona": "bruce" }` and `records` contains only matching aggregated data

## Error Conditions

**Error Scenario 1: --output path is not writable**
- **Given** `--output /root/protected/file.json` targets a directory the user cannot write to
- **When** `atcr leaderboard --export --output /root/protected/file.json` is executed
- **Then** the command exits with code 1 and prints a clear error message indicating the path is not writable; no partial file is left behind

**Error Scenario 2: --output path is a directory**
- **Given** `--output /tmp/` points to an existing directory
- **When** `atcr leaderboard --export --output /tmp/` is executed
- **Then** the command exits with code 1 and prints an error indicating the path must be a file, not a directory

## Performance Requirements
- **Response Time:** Export completes in < 2 seconds for up to 10,000 aggregated records
- **Memory:** Export does not load more than 2x the JSONL store size into memory simultaneously

## Security Considerations
- **No network calls:** Export is purely local file generation; no HTTP requests are made
- **Output atomicity:** `--output` writes to a temp file then renames, preventing partial writes on crash
- **Path validation:** Output path is validated for writability before any data processing begins

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Fixture aggregated records (as `[]ScorecardRecord`) with known metrics
- Golden-file JSON for expected v1 schema output

**Mock/Stub Requirements:**
- Stub the scorecard store reader to return fixture records from memory (no filesystem I/O)
- Use `os.TempDir()` for `--output` tests; verify file content via `os.ReadFile`
- Capture stdout via `bytes.Buffer` for stdout export tests

## Definition of Done

**Auto-Verified:**
- [ ] `go test ./internal/scorecard/... ./cmd/atcr/...` passes
- [ ] `go vet ./internal/scorecard/... ./cmd/atcr/...` clean
- [ ] `go build ./cmd/atcr/...` succeeds
- [ ] `atcr leaderboard --help` lists `--export` and `--output` flags

**Story-Specific:**
- [ ] `Export()` function in `internal/scorecard/export.go` returns valid v1 schema JSON
- [ ] `schema_version` field is integer `1` in all output
- [ ] `exported_at` field is RFC 3339 timestamp
- [ ] `filters` object reflects the active `--since`, `--model`, `--persona` values
- [ ] `--output <path>` writes to file; without it, JSON goes to stdout
- [ ] Exit code 0 on success; exit code 1 on output path errors

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Export function follows existing patterns in `internal/scorecard/` package

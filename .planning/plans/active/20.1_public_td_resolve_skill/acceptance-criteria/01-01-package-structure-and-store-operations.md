# Acceptance Criteria: Package Structure and Store Operations

**Related User Story:** [01: Local TD Store Persistence](../user-stories/01-local-td-store-persistence.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/localdebt`) | Stdlib-only, no new dependency |
| Test Framework | `go test` with `testify/require`/`assert` | Matches `internal/scorecard/store_test.go` precedent |
| Key Dependencies | `encoding/json`, `os`, `path/filepath`, `bufio` (stdlib only) | No third-party JSONL/YAML libs |

### Related Files (from codebase-discovery.json)
- `internal/localdebt/store.go` — create: `Append(dir string, rec Record) error`, `ReadRecords(path string, opts ReadOpts) ([]Record, error)`, `ReadAll(dir string, opts ReadOpts) ([]Record, error)`, structurally copied from `internal/scorecard/store.go`
- `internal/localdebt/record.go` — create: `Record` struct matching the v1 schema, plus `SchemaVersion` constant
- `internal/localdebt/paths.go` — create: `.atcr/debt/` root resolution and `monthFromRunID`-equivalent shard derivation, adapted from `internal/scorecard/paths.go`
- `internal/scorecard/store.go` — reference (read-only): structural precedent for Append/ReadRecords/ReadAll; atomic single-write-per-record append at line 89
- `internal/scorecard/paths.go` — reference (read-only): `DefaultDir` and `monthFromRunID` path/shard resolution at line 23
- `internal/scorecard/store_test.go` — reference (read-only): test patterns for append/read round-trip and concurrent-append safety
- `docs/scorecard.md` — reference (read-only): documentation style precedent for the local JSONL store format
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/local-td-store-schema.md` — reference: v1 record schema definition
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/append-only-store-pattern.md` — reference: atomic-append and malformed-line-skip pattern requirements

## Happy Path Scenarios
**Scenario 1: Append then read back a single record**
- **Given** an empty `.atcr/debt/` directory (or a fresh `t.TempDir()` standing in for it in tests) and a valid `Record` with `run_id` set to `"2026-06-14T10:00:00Z-abc123"`
- **When** `Append(dir, rec)` is called, then `ReadRecords(filepath.Join(dir, "2026-06.jsonl"), ReadOpts{})` is called
- **Then** the call returns one record, byte-for-byte equivalent (every field, including optional fields) to the record passed to `Append`

**Scenario 2: Two separate Append calls do not tear or interleave**
- **Given** an empty store directory
- **When** two records are appended via two separate `Append(dir, rec)` calls with the same `run_id` month
- **Then** the resulting `YYYY-MM.jsonl` file contains exactly two well-formed JSON lines, each independently parseable, in append order

**Scenario 3: ReadAll aggregates every month shard**
- **Given** a store directory containing `2026-06.jsonl` (1 record) and `2026-07.jsonl` (1 record), plus a non-`.jsonl` file (e.g. `notes.txt`)
- **When** `ReadAll(dir, ReadOpts{})` is called
- **Then** it returns 2 records total (both months), and the non-JSONL file is silently ignored

## Edge Cases
**Edge Case 1: Directory created lazily on first write**
- **Given** a store root that does not yet exist on disk
- **When** `Append(dir, rec)` is called for the first time
- **Then** `dir` is created (`MkdirAll`, mode `0700`) as a side effect, and the record is written successfully — no error for "directory does not exist"

**Edge Case 2: ReadAll on a missing `.atcr/debt/` directory**
- **Given** a store directory path that does not exist on disk (fresh repo, `atcr reconcile` never run)
- **When** `ReadAll(dir, ReadOpts{})` is called
- **Then** it returns `(nil, nil)` — no error, empty slice — matching `internal/scorecard/store.go`'s `ReadAll` missing-directory behavior

**Edge Case 3: Month-boundary run splits records across two shard files**
- **Given** a store directory with no existing files
- **When** one record is appended with `run_id` `"2026-06-30T23:59:00Z-jun"` and a second with `run_id` `"2026-07-01T00:01:00Z-jul"`
- **Then** `2026-06.jsonl` and `2026-07.jsonl` both exist, each containing its own record

## Error Conditions
**Error Scenario 1: Append with a run_id that has no derivable YYYY-MM prefix**
- **Given** a `Record` whose `run_id` does not start with a valid `YYYY-MM` prefix (e.g. `"not-a-run-id"`)
- **When** `Append(dir, rec)` is called
- Error message: `"cannot derive month from run_id \"not-a-run-id\" (expected YYYY-MM prefix)"` (mirroring `monthFromRunID`'s error shape)
- HTTP status / error code: N/A (Go `error` return, not an HTTP boundary)

**Error Scenario 2: Append target path blocked by a non-directory file**
- **Given** a store directory path where an ancestor component is a regular file (not a directory), so `MkdirAll` cannot succeed
- **When** `Append(dir, rec)` is called
- Error message: wrapped `*os.PathError`-derived message containing the operational context (e.g. `"creating localdebt dir: ..."`) with the absolute path reduced to its base name (matching `basePathErr` in `internal/scorecard/store.go:22`) so a username-bearing path is never embedded
- HTTP status / error code: N/A (Go `error` return)

## Performance Requirements
- **Response Time:** `Append` completes in a single `os.OpenFile` + one `os.Write` per record — no measurable per-call overhead beyond one syscall round-trip; no benchmark target required at this record volume (~500 bytes/record, consistent with the scorecard precedent).
- **Throughput:** `ReadRecords`/`ReadAll` stream-parse via `bufio.Reader` line-by-line rather than loading the whole file into memory in one buffer, matching the scorecard precedent's "no load-entire-file-into-memory" intent; a returned `[]Record` slice materializing parsed records is expected and acceptable at documented scale.

## Security Considerations
- **Authentication/Authorization:** N/A — local filesystem operations only, no network or auth boundary.
- **Input Validation:** `run_id` must resolve to a valid `YYYY-MM` prefix before any file is opened, preventing path-traversal via a malformed `run_id` (mirrors `internal/scorecard/paths.go`'s `monthRe` validation). Store directory and file are created with `0700`/`0600` permissions respectively so records are not world- or group-readable.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A `sampleRecord(runID string) Record` test helper analogous to `internal/scorecard/store_test.go`'s `sampleRecord`, populating all required v1 fields with minimal valid values.
**Mock/Stub Requirements:** None — use `t.TempDir()` as the store root for every test (no filesystem mocking needed, matching the scorecard test precedent).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/localdebt` package exists with `Record`, `Append`, `ReadRecords`, `ReadAll` matching the signatures above
- [ ] Append/read round-trip test passes with byte-for-byte field equivalence
- [ ] `ReadAll` on a missing directory returns `(nil, nil)`
- [ ] Month-boundary sharding test (two `run_id`s spanning a month boundary produce two separate shard files) passes

**Manual Review:**
- [ ] Code reviewed and approved

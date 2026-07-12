# Acceptance Criteria: Tolerant Read Path (Malformed Lines and Schema Versioning)

**Related User Story:** [01: Local TD Store Persistence](../user-stories/01-local-td-store-persistence.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/localdebt`) | `bufio.Reader`-based streaming parse, not `bufio.Scanner` |
| Test Framework | `go test` with `testify/require`/`assert` | |
| Key Dependencies | `bufio`, `encoding/json`, `io` (stdlib only) | |

## Related Files
- `internal/localdebt/store.go` - modify: `ReadRecords` line-streaming loop with malformed-line and schema-version skip logic, copied structurally from `internal/scorecard/store.go:132-197` (`ReadRecords`/`decodeRecord`)
- `internal/localdebt/record.go` - reference: `Record.SchemaVersion` field and package `SchemaVersion` constant used by the version-negotiation check
- `internal/scorecard/store.go` - reference (read-only): `decodeRecord`, `drainLine`, and the `maxLineBytes` over-long-line guard to mirror
- `internal/scorecard/store_test.go` - reference (read-only): `TestStore_ReadRecords_SkipsMalformedLines` and `TestStore_ReadRecords_SkipsFutureSchemaVersion` as the test pattern to replicate

## Happy Path Scenarios
**Scenario 1: A clean multi-line file reads every record**
- **Given** a shard file containing 3 well-formed JSONL records
- **When** `ReadRecords(path, ReadOpts{})` is called
- **Then** it returns all 3 records in file order, with no diagnostics written

## Edge Cases
**Edge Case 1: A malformed line is skipped, valid lines on both sides are retained**
- **Given** a shard file with a valid record, then a line `{not valid json`, then another valid record
- **When** `ReadRecords(path, ReadOpts{})` is called
- **Then** it returns exactly 2 records (the malformed line skipped) and no error is returned; a warning naming the malformed line is written to the diagnostics writer

**Edge Case 2: A record with schema_version greater than current is skipped**
- **Given** a shard file with one v1 record and one record whose `schema_version` is `SchemaVersion + 1`
- **When** `ReadRecords(path, ReadOpts{})` is called
- **Then** it returns exactly 1 record (only the v1 one), the future-schema record is never unmarshaled into the current `Record` struct, and a warning is written to the diagnostics writer

**Edge Case 3: An over-long line does not abort the read**
- **Given** a shard file with a valid record, then one line exceeding the package's line-length cap, then another valid record
- **When** `ReadRecords(path, ReadOpts{})` is called
- **Then** it returns both valid records (before and after the oversized line), no error, and a warning about the skipped over-long line is written to the diagnostics writer

**Edge Case 4: Diagnostics route to the injected ReadOpts.Writer, not os.Stderr**
- **Given** a shard file containing a malformed line and a `ReadOpts{Writer: &buf}` with a caller-supplied `*bytes.Buffer`
- **When** `ReadRecords(path, ReadOpts{Writer: &buf})` is called
- **Then** the malformed-line warning text appears in `buf`, not on the process's real stderr

**Edge Case 5: A nil Writer defaults to os.Stderr without panicking**
- **Given** a shard file with one malformed line and `ReadOpts{}` (zero value, nil `Writer`)
- **When** `ReadRecords(path, ReadOpts{})` is called
- **Then** the read completes successfully (valid records returned, no panic) — the diagnostic falls back to `os.Stderr`

## Error Conditions
**Error Scenario 1: A genuinely missing shard file surfaces as the raw os error**
- **Given** a `path` that does not exist on disk
- **When** `ReadRecords(path, ReadOpts{})` is called
- Error message: the raw `*os.PathError` from `os.Open` (e.g. `"open <path>: no such file or directory"`), unwrapped and un-redacted, so callers can distinguish "no records" via `os.IsNotExist`
- HTTP status / error code: N/A (Go `error` return)

## Performance Requirements
- **Response Time:** Parsing is single-pass, line-by-line via `bufio.Reader` — no full-file buffering, no re-reading on a skip.
- **Throughput:** An over-long line (exceeding the configured cap) must be drained without allocating an unbounded buffer, matching `internal/scorecard/store.go`'s `maxLineBytes` (1 MiB) precedent.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** Malformed or forward-incompatible lines are never partially trusted — a line that fails `json.Unmarshal` or carries an unsupported `schema_version` is discarded wholesale, never merged into a zero-valued `Record` and returned as if valid.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Hand-constructed JSONL fixtures mixing valid records, a malformed JSON line, a future-`schema_version` record, and (for the over-long-line case) a line built via `bytes.Repeat` past the cap — mirroring `internal/scorecard/store_test.go`'s fixture construction exactly.
**Mock/Stub Requirements:** A `*bytes.Buffer` passed as `ReadOpts.Writer` to capture and assert on diagnostic output; no other mocking needed.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Malformed-line skip test passes (valid records on both sides retained, no error)
- [ ] Future-schema-version skip test passes (record not misread as current version)
- [ ] Over-long-line skip test passes (read continues past the oversized line)
- [ ] Diagnostics-routing test passes (warnings land on injected `ReadOpts.Writer`, default `os.Stderr` on nil)

**Manual Review:**
- [ ] Code reviewed and approved

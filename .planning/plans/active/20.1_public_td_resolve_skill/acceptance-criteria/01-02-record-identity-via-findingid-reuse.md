# Acceptance Criteria: Record Identity via FindingID Reuse

**Related User Story:** [01: Local TD Store Persistence](../user-stories/01-local-td-store-persistence.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/localdebt`) | Imports `internal/history` for `FindingID` only |
| Test Framework | `go test` with `testify/require`/`assert` | |
| Key Dependencies | `internal/history` (`FindingID` function only) | Stdlib SHA-256 under the hood, no new dependency |

## Related Files
- `internal/localdebt/record.go` - modify: `Record.ID` field populated via `history.FindingID(file, line, problem)`, imported from `internal/history`
- `internal/localdebt/store.go` - modify (or a small helper) - the write path that constructs a `Record` and stamps `ID` before `Append`
- `internal/history/record.go` - reference (read-only): `FindingID` at line 48 — SHA-256 over NUL-separated `file\x00line\x00problem`, first 8 bytes hex-encoded
- `.planning/plans/active/20.1_public_td_resolve_skill/documentation/local-td-store-schema.md` - reference: "Identity and Deduplication" section documenting the ID construction and severity exclusion

## Happy Path Scenarios
**Scenario 1: Two records with identical file/line/problem share the same ID**
- **Given** two `Record` values built for the same `file`, `line`, and `problem`, but from two different reconcile runs (different `run_id`/`ts`) and possibly different `severity`
- **When** each record's `ID` is computed via `history.FindingID(file, line, problem)`
- **Then** both records have the identical `ID` string (16 hex characters, matching `history.FindingID`'s 8-byte digest)

**Scenario 2: localdebt.Record.ID matches history.FindingID output exactly**
- **Given** a fixed `file`, `line`, `problem` triple
- **When** `history.FindingID(file, line, problem)` is called directly, and separately a `localdebt.Record` is constructed with the same inputs
- **Then** `record.ID == history.FindingID(file, line, problem)` — the package does not reimplement or diverge from the shared hash construction

## Edge Cases
**Edge Case 1: Severity change does not change ID**
- **Given** a finding first reconciled at `severity: "MEDIUM"` and later re-reconciled (after a debate/verify re-settle) at `severity: "HIGH"` with the same `file`/`line`/`problem`
- **When** both records' `ID`s are computed
- **Then** the two IDs are identical — severity is deliberately excluded from the ID construction, matching `history.FindingID`'s documented contract

**Edge Case 2: Problem text carrying a symbol anchor prefix is part of the hash input**
- **Given** a `problem` string stamped with a `(symbolName)` anchor prefix (per Epic 18.1 / `docs/technical-debt-format.md`)
- **When** the `ID` is computed
- **Then** the full `problem` string including the anchor is hashed verbatim — no anchor-stripping or normalization occurs before hashing, so an unchanged anchor+problem yields a stable ID across runs

## Error Conditions
**Error Scenario 1: Empty problem string still yields a deterministic (not panicking) ID**
- **Given** a `Record` with `problem == ""` (a degenerate/malformed reconciled finding)
- **When** `history.FindingID(file, line, "")` is called
- Error message: N/A — `FindingID` has no error return; it always yields a deterministic hex digest, never panics on empty input
- HTTP status / error code: N/A (pure function, no I/O)

## Performance Requirements
- **Response Time:** `FindingID` is a single SHA-256 computation over a short byte string (~100 bytes typical); negligible per-record cost, no batching or caching required.
- **Throughput:** N/A — ID computation is O(1) per record and does not scale with store size.

## Security Considerations
- **Authentication/Authorization:** N/A.
- **Input Validation:** No new validation introduced — `FindingID` accepts any `string`/`int`/`string` triple; the store does not attempt to sanitize `file`/`problem` beyond what `internal/history` already accepts, preserving identical behavior to the existing consumer (`internal/debate.itemID`, `internal/history`).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Reuse `internal/history`'s existing `FindingID` test fixtures/expectations as a cross-check; no new hash test vectors need to be invented — the localdebt tests assert *equality* with `history.FindingID`'s output, not a redefinition of the hash.
**Mock/Stub Requirements:** None — `history.FindingID` is a pure function, called directly (not mocked), which is itself the point of this AC: it must be the real imported function, not a local reimplementation.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/localdebt` imports `internal/history` for `FindingID` only (verified by code review / import audit — no `internal/history` read/write functions such as `Append`, `ShardDir`, or `LegacyLedgerPath` are called)
- [ ] Test confirms `localdebt` record `ID` equals `history.FindingID(file, line, problem)` for the same inputs
- [ ] Test confirms severity changes do not change `ID` for otherwise-identical `file`/`line`/`problem`

**Manual Review:**
- [ ] Code reviewed and approved

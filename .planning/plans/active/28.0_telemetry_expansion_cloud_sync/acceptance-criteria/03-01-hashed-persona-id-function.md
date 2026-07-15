# Acceptance Criteria: Deterministic Hashed-Persona-ID Function

**Related User Story:** [03: Persona ID Hashing for the Persona Leaderboard](../user-stories/03-persona-id-hashing-for-leaderboard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package function (`internal/scorecard`) | Pure, stateless building block; no I/O |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Matches existing `internal/scorecard/export_test.go` conventions |
| Key Dependencies | Go stdlib `crypto/sha256`, `encoding/hex` | No new external dependency, per plan.md's stdlib-only package list |

## Related Files
- `internal/scorecard/telemetry.go` - create: defines `HashPersonaID(raw string) string`, a hex-encoded SHA-256 digest function kept in a file separate from `export.go`'s `PublicRecord`/`scrubField`/`AnonymizeRecord`/`ScrubPublicRecord` boundary.
- `internal/scorecard/export.go` - reference only (not modified by this AC): `AnonymizeRecord` (line ~143) establishes the "scrub/hash once, at ingestion" shape this function reuses; `scrubField` (line ~321) and `PublicRecord` (line ~35) must remain untouched.
- `internal/scorecard/scorecard.go` - reference only (not modified by this AC): `Record.Reviewer` (line ~56) is the raw Persona ID source field this function hashes.

## Happy Path Scenarios
**Scenario 1: Hash a real Persona ID**
- **Given** a `Record` with `Reviewer` set to `"skeptical-reviewer"`
- **When** `HashPersonaID(record.Reviewer)` is called
- **Then** it returns a 64-character lowercase hex string (SHA-256 digest) that does not equal, contain, or resemble the input string `"skeptical-reviewer"`

**Scenario 2: Function lives outside the Epic 10.0 boundary**
- **Given** the `internal/scorecard` package source
- **When** `HashPersonaID` is inspected
- **Then** it is defined in `telemetry.go` (not `export.go`), carries a doc comment explicitly stating it is NOT part of the `PublicRecord` allowlist path, and does not call, wrap, or reference `scrubField`, `PublicRecord`, `AnonymizeRecord`, or `ScrubPublicRecord`

## Edge Cases
**Edge Case 1: Empty Persona ID**
- **Given** `raw = ""`
- **When** `HashPersonaID("")` is called
- **Then** it returns the well-defined SHA-256 hex digest of the empty string (`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`) rather than panicking or returning an empty/zero value

**Edge Case 2: Persona ID containing PII-like or path-like substrings**
- **Given** `raw = "/Users/sam/reviewer"` (a value that would trigger `scrubField`'s path stripping on the public leaderboard path)
- **When** `HashPersonaID(raw)` is called
- **Then** the function performs no scrubbing/stripping pre-processing of its own — it hashes the raw byte value as given, since the hash itself (not text-scrubbing) is the non-reversibility guarantee for this separate schema

**Edge Case 3: Unicode / non-ASCII Persona ID**
- **Given** `raw` contains multi-byte UTF-8 characters (e.g. `"审阅者-42"`)
- **When** `HashPersonaID(raw)` is called
- **Then** it returns a valid 64-character hex digest with no error or truncation (Go strings are UTF-8 byte sequences; `sha256.Sum256` hashes bytes, not runes)

## Error Conditions
**Error Scenario 1: N/A — pure function**
- `HashPersonaID` has signature `func HashPersonaID(raw string) string` and returns no error; SHA-256 hashing over an arbitrary Go `string` (byte sequence) cannot fail, so there is no error path to test
- No panics are permitted for any string input, including empty string and strings containing null bytes or invalid UTF-8 byte sequences

## Performance Requirements
- **Response Time:** A single call completes in well under 1ms for Persona ID strings of realistic length (< 256 bytes); no measurable overhead added to the reconcile/export hot path since this function is not yet wired into either.
- **Throughput:** Stateless and allocation-light; safe to call repeatedly per record without caching (SHA-256 over a short string is cheap enough that memoization is unnecessary).

## Security Considerations
- **Authentication/Authorization:** N/A — pure hashing function, no I/O, no external calls.
- **Input Validation:** Accepts any Go `string` (including empty and non-ASCII) without validation or rejection; hashing is total over the input domain. No raw Persona ID value is logged, wrapped in an error, or otherwise surfaced by this function — it only returns the digest.
- **Non-reversibility:** SHA-256 is a one-way cryptographic hash; the function must not retain, cache, or log the raw input anywhere reachable after the call returns.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A handful of representative Persona ID strings: a typical name, empty string, a path-like string, a Unicode string, and two near-identical strings differing by one character (to spot-check avalanche behavior is out of scope but difference in output is in scope — covered fully by AC 03-04).
**Mock/Stub Requirements:** None — pure function with no external dependencies to mock.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `HashPersonaID(raw string) string` exists in `internal/scorecard/telemetry.go` and returns a hex-encoded SHA-256 digest
- [ ] Doc comment explicitly states the function is separate from the `PublicRecord`/`scrubField` boundary
- [ ] No call from `HashPersonaID` into `scrubField`, `PublicRecord`, `AnonymizeRecord`, or `ScrubPublicRecord`
- [ ] Empty-string and Unicode inputs produce valid 64-character hex output without panic

**Manual Review:**
- [ ] Code reviewed and approved

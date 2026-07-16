# Acceptance Criteria: Hash Determinism, Uniqueness, and Non-Reversibility Unit Tests

**Related User Story:** [03: Persona ID Hashing for the Persona Leaderboard](../user-stories/03-persona-id-hashing-for-leaderboard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go unit test (`internal/scorecard`) | Verifies `HashPersonaID`'s cryptographic properties from AC 03-01 |
| Test Framework | `go test` + `testify` (`assert`/`require`) | |
| Key Dependencies | None new — tests the stdlib-backed `HashPersonaID` function directly | |

### Related Files (from codebase-discovery.json)
- `internal/scorecard/telemetry_test.go` - create: unit tests for `HashPersonaID` covering determinism, uniqueness, and non-reversibility.
- `internal/scorecard/telemetry.go` - reference only (implemented by AC 03-01): the `HashPersonaID(raw string) string` function under test.

## Happy Path Scenarios
**Scenario 1: Determinism — same input yields the same hash across calls**
- **Given** the Persona ID string `"bruce"`
- **When** `HashPersonaID("bruce")` is called twice (and again in a separate `*testing.T` subtest simulating a fresh call, standing in for a process restart since there is no per-run salt or seeded RNG)
- **Then** both calls return the identical hash string

**Scenario 2: Uniqueness — different inputs yield different hashes**
- **Given** two distinct Persona ID strings `"bruce"` and `"alice"`
- **When** `HashPersonaID` is called on each
- **Then** the two returned hash strings are not equal

## Edge Cases
**Edge Case 1: Near-identical inputs still diverge**
- **Given** `"bruce"` and `"bruce "` (trailing space) or `"Bruce"` (case difference)
- **When** each is hashed
- **Then** the outputs differ (SHA-256 has no case-folding or whitespace-trimming behavior — the function performs no normalization), confirming the function is a byte-exact hash rather than a fuzzy/normalized match

**Edge Case 2: Repeated calls across a table-driven test with 20+ distinct Persona IDs**
- **Given** a slice of at least 20 distinct sample Persona ID strings
- **When** each is hashed once
- **Then** all resulting hashes are pairwise distinct (asserted via a `map[string]bool` seen-set with no collisions) and each call, when repeated, reproduces its own prior hash — combining the determinism and uniqueness checks at scale

## Error Conditions
**Error Scenario 1: Non-reversibility — raw string never appears in output or logs**
- **Given** a distinctive Persona ID string, e.g. `"correct-horse-battery-staple-42"`
- **When** `HashPersonaID` is called and its return value is captured, and (separately) any log/error/diagnostic output producible on this hashing path is captured
- **Then** the raw string `"correct-horse-battery-staple-42"` does not appear as a substring anywhere in the returned hash value, and does not appear in any log line or error string this path can produce (there are none today — `HashPersonaID` returns no error and has no Diag/logging hook — and the test asserts this remains true by construction: the function signature has no `error` return and no `io.Writer` parameter)
- Error message: N/A — this is a negative assertion (`assert.NotContains`), not an error-path test

## Performance Requirements
- **Response Time:** The full test suite (including the 20+ entry table-driven test) completes in well under 100ms — SHA-256 hashing of short strings is effectively instantaneous.
- **Throughput:** N/A — unit test, not a load/benchmark test (a `go test -bench` benchmark is optional and not required for this AC).

## Security Considerations
- **Authentication/Authorization:** N/A — local unit test, no network/auth surface.
- **Input Validation:** Tests assert the function accepts arbitrary strings (including edge cases from AC 03-01: empty string, Unicode) without panicking, reinforcing that `HashPersonaID` has no hidden validation that could reject or alter a real Persona ID unexpectedly.
- **Cryptographic guarantee:** These tests are the story's direct evidence for the epic's non-reversibility requirement — they must fail if a future refactor swaps SHA-256 for a reversible encoding (e.g. base64) or a lossy/truncated hash that increases collision risk.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A table-driven test with at least 20 distinct Persona ID strings covering typical names, empty string, Unicode, whitespace variants, and case variants; no fixtures shared with `export_test.go` are required (this test is fully self-contained).
**Mock/Stub Requirements:** None — `HashPersonaID` is a pure function with no dependencies to mock.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Determinism test: same input produces the same hash across repeated calls
- [x] Uniqueness test: distinct inputs (including near-identical strings) produce distinct hashes across a 20+ entry table with no collisions
- [x] Non-reversibility test: the raw input string never appears as a substring of the hash output
- [x] Edge cases (empty string, Unicode, whitespace/case variants) all pass without panic

**Manual Review:**
- [ ] Code reviewed and approved

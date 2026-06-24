# Acceptance Criteria: Silent Fallback When No Language Match

**Related User Story:** [03: Language-Aware Skeptic Routing](../user-stories/03-language-aware-skeptic-routing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Fallback behavior | Go package (`internal/verify`) | No log, no error when no match |
| Test Framework | go test / testify | Unit tests asserting silent fallback |
| Key Dependencies | `internal/verify/select.go`, standard `log` package (must NOT be called) | |

## Related Files
- `internal/verify/select.go:55` - modify: ensure the two-partition reorder does not emit any log output or return any error when no skeptic language matches the finding extension
- `internal/verify/select_test.go` - modify: add test cases verifying that fallback to the full pool is silent and produces output identical to pre-routing alphabetical behavior

## Happy Path Scenarios

**Scenario 1: No skeptic has a Language field set — output matches pre-routing baseline**
- **Given** a pool of skeptics `["alpha", "bravo", "charlie"]` all with `Language = nil` and `n=2`
- **When** `SelectEligibleSkeptics` is called with `finding.File = "main.go"`
- **Then** the returned slice is `["alpha", "bravo"]` — identical to alphabetical selection before routing was introduced

**Scenario 2: Skeptics have Language set but none match the file extension — full pool used**
- **Given** skeptics `["idiomatic"]` with `Language = ["go"]` and finding `File = "auth.ts"`
- **When** `SelectEligibleSkeptics` is called with `n=1`
- **Then** `idiomatic` is returned (only available skeptic); no error, no log output; behavior is indistinguishable from pre-routing run where the skeptic had no language filter

**Scenario 3: All skeptics have Language set to empty slice — full pool used**
- **Given** skeptics `["alpha", "bravo"]` both with `Language = []` and `n=2`
- **When** `SelectEligibleSkeptics` is called with `finding.File = "util.go"`
- **Then** the returned slice is `["alpha", "bravo"]` (alphabetical); no language matching attempted; no log, no error

## Edge Cases

**Edge Case 1: Mixed pool — some have Language, none match — unmatched pool fills the cap**
- **Given** skeptics `["delta"]` with `Language = ["ts"]` and `["alpha", "bravo"]` with no language, `n=2`, `finding.File = "handler.rb"`
- **When** `SelectEligibleSkeptics` is called
- **Then** the returned slice is `["alpha", "bravo"]` (unmatched partition fills n=2); `delta` is excluded only because it is third after partition reorder with n=2; no error

**Edge Case 2: n larger than total pool — all skeptics returned regardless of match**
- **Given** total pool of 3 skeptics with 1 matched, 2 unmatched, `n=10`
- **When** `SelectEligibleSkeptics` is called
- **Then** all 3 are returned (matched first, then unmatched); no error

## Error Conditions

**Error Scenario 1: No log output on fallback (negative assertion)**
- **Condition:** `SelectEligibleSkeptics` must NOT call `log.Print`, `log.Printf`, or any logging function when falling back to the general pool
- Error message: N/A — absence of log output is the requirement
- HTTP status / error code: Any log output detected in tests is a test failure

## Performance Requirements
- **Response Time:** The no-match code path (all unmatched) must be as fast as pre-routing behavior; the matched slice will be empty (zero allocation) and the append collapses to a no-op.
- **Throughput:** No degradation over pre-routing baseline for workloads with no language-scoped personas installed.

## Security Considerations
- **Input Validation:** No input validation is added in this path; the fallback is purely a slice reorder result (empty matched partition).
- **Authentication/Authorization:** No auth impact.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table entries covering: all-nil Language pool, all-empty-slice Language pool, mixed pool with language set but no match for the given extension, n > pool size.
**Mock/Stub Requirements:** No mocks required. Capture log output using `log.SetOutput` + `bytes.Buffer` during the test to assert zero bytes are written during fallback scenarios.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/verify/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Unit tests confirm output is byte-for-byte identical to pre-routing alphabetical selection when no language match exists
- [ ] No `log.*` calls appear in the fallback code path (verified by test capturing log output)
- [ ] Pool with all-empty `Language` fields produces same result as pool with all-nil `Language` fields

**Manual Review:**
- [ ] Code reviewed and approved

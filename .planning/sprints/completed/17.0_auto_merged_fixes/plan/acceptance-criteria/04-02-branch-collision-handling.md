# Acceptance Criteria: Branch Name Collision Is Distinguishable and Recoverable

**Related User Story:** [Story 4: Create a Branch and Commit the Verified Fix](../user-stories/04-create-branch-and-commit-verified-fix.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client.CreateBranch`) + caller-facing error inspection | Uses the existing `*ghaction.APIError.StatusCode` field — no new error type |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Same harness as 04-01 |
| Key Dependencies | `internal/ghaction.APIError` (existing), `errors.As` | Standard library only |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: `CreateBranch` returns the `*ghaction.APIError` unchanged on a 422 response (no swallowing/wrapping that hides `StatusCode`) so a caller can `errors.As` it and inspect `StatusCode == 422`
- `internal/ghaction/client_test.go` - modify: add `TestCreateBranchRefAlreadyExists` asserting the returned error is a `*ghaction.APIError` with `StatusCode == 422` and a message containing GitHub's "Reference already exists" text

## Happy Path Scenarios
**Scenario 1: Caller distinguishes a collision from other failures**
- **Given** `CreateBranch` returns an error from a stub server responding 422 with body `{"message":"Reference already exists"}`
- **When** the caller inspects the returned error with `errors.As(err, &apiErr)`
- **Then** `apiErr.StatusCode == 422` is true and the caller can branch on this specific condition (retry with a suffixed name, or surface a clear "branch already exists" message) rather than treating it as a generic failure

## Edge Cases
**Edge Case 1: A 422 for an invalid SHA is not misclassified as a name collision**
- **Given** a 422 response body reading `{"message":"Object does not exist"}` (invalid base SHA, not a ref collision)
- **When** the caller inspects `apiErr.Message`
- **Then** the message text (not just the status code) is available for the caller to distinguish "already exists" from "invalid SHA" — `CreateBranch` does not collapse the two into the same generic error, since the plan's mitigation for collisions (retry-with-suffix) does not apply to an invalid-SHA failure

**Edge Case 2: Deterministic branch naming reduces but does not eliminate collisions**
- **Given** the caller (Story 6's `--auto-fix` orchestrator) generates branch names from a stable prefix plus a timestamp/finding-identifier per the story's constraint
- **When** two auto-fix runs are triggered within the same identifier window (e.g. a retried CI run)
- **Then** the second `CreateBranch` call still surfaces the 422 cleanly rather than silently succeeding or corrupting the first branch — this AC covers only that the `Client` surfaces the condition correctly; the retry-with-suffix decision itself is out of scope for `internal/ghaction` and belongs to the Story 6 CLI orchestrator

## Error Conditions
**Error Scenario 1: Ref already exists**
- Error message: `"github API returned 422: Reference already exists"`
- HTTP status / error code: 422, exposed via `APIError.StatusCode`

**Error Scenario 2: Invalid base SHA (distinct condition, same status code)**
- Error message: `"github API returned 422: Object does not exist"`
- HTTP status / error code: 422 — same code as collision; callers needing to distinguish the two must inspect `APIError.Message`, which this AC guarantees is preserved verbatim (redacted only for secrets, never rewritten for shape)

## Performance Requirements
- **Response Time:** No additional round trip beyond the single `CreateBranch` call — the 422 is returned on the first response, not discovered via a follow-up GET
- **Throughput:** N/A (single-call condition)

## Security Considerations
- **Authentication/Authorization:** N/A beyond 04-01 (same auth path)
- **Input Validation:** The 422 message body still passes through `redactSecrets` before being embedded in the error, so a collision error can never leak a token even if GitHub's error body were to echo request headers

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Two stub server responses — 422 with "Reference already exists" and 422 with "Object does not exist" — asserted as distinct via `APIError.Message` content
**Mock/Stub Requirements:** `httptest.NewServer` returning canned 422 bodies per sub-test; no live GitHub interaction

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] A 422 "Reference already exists" response is returned to the caller as a `*ghaction.APIError` with `StatusCode == 422` and the original GitHub message intact
- [x] A 422 for a different cause (invalid SHA) is distinguishable by message text, not conflated with a collision
- [x] No retry-with-suffix logic is implemented inside `internal/ghaction` — that decision remains the caller's, per the story's constraint that `CreateBranch`/`CreateCommit` are pure API wrappers

**Manual Review:**
- [x] Code reviewed and approved

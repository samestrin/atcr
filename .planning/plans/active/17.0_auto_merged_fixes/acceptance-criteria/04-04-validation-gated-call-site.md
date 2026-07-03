# Acceptance Criteria: CreateBranch/CreateCommit Are Unreachable Without a Prior Validation Success

**Related User Story:** [Story 4: Create a Branch and Commit the Verified Fix](../user-stories/04-create-branch-and-commit-verified-fix.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go orchestration code in the new `internal/autofix` package (Story 1's home package) | This AC is about the *call site*, not the `ghaction.Client` methods themselves — per the story's AC-3 overview item, the ordering guarantee lives in the caller, not the `Client` |
| Test Framework | Go `testing` + `httptest.Server` asserting zero requests | The test's key assertion is an absence of HTTP calls, not a response shape |
| Key Dependencies | Story 2's validation-result type (pass/fail), Story 4's `ghaction.Client.CreateBranch`/`CreateCommit` | Cross-story dependency: this AC cannot be fully exercised until Story 2's validation-result type exists, but the `internal/ghaction` half (04-01/04-02/04-03) is independently testable now |

## Related Files
- `internal/autofix/orchestrator.go` - create/modify: the single function or method that sequences validate → (on success) branch+commit is structured so `ghaction.Client.CreateBranch`/`CreateCommit` calls are reachable only from the success branch of the validation result's handling — e.g. a `switch result.Status { case ValidationPassed: ... case ValidationFailed: return revertAndReport(...) }` with no fallthrough path into the GitHub-mutating branch
- `internal/autofix/orchestrator_test.go` - create/modify: `TestOrchestratorNeverCallsGitHubOnValidationFailure` using an `httptest.Server` that fails the test (via `t.Fatal` in the handler or a request counter asserted to be 0) if `/git/refs` or `/git/blobs` receives any request when the validation stub reports failure

## Happy Path Scenarios
**Scenario 1: Validation success unlocks the GitHub-mutating call**
- **Given** Story 2's validation step returns a passing result for the current working-tree state
- **When** the `internal/autofix` orchestrator processes that result
- **Then** it proceeds to call `ghaction.Client.CreateBranch` followed by `CreateCommit`, using the token/repo/branch-name inputs it already holds — this is the only code path in the orchestrator with access to a live `*ghaction.Client` capable of invoking these two methods

## Edge Cases
**Edge Case 1: Validation failure short-circuits before any GitHub-capable client is constructed**
- **Given** Story 2's validation step returns a failing result
- **When** the orchestrator processes that result
- **Then** it invokes Story 3's revert path and returns without ever constructing or invoking a `*ghaction.Client` call to `/git/refs` or `/git/blobs` — the test asserts zero requests reached the stub server, not merely that the returned error is non-nil

**Edge Case 2: Validation step itself errors (distinct from "ran and failed")**
- **Given** Story 2's validation step returns an error (e.g. the build tool itself could not run) rather than a clean pass/fail result
- **When** the orchestrator processes that outcome
- **Then** it is treated the same as a validation failure for the purposes of this gate — an inconclusive validation never unlocks the GitHub-mutating branch

## Error Conditions
**Error Scenario 1: A future code change accidentally moves the GitHub call outside the success branch**
- Error message: N/A — this is a structural/test guarantee, not a runtime error message
- HTTP status / error code: N/A — enforced by `TestOrchestratorNeverCallsGitHubOnValidationFailure` failing the build if the invariant is violated, not by a runtime check

## Performance Requirements
- **Response Time:** N/A — this AC is a control-flow guarantee, not a latency requirement
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** The token/repo credentials are only ever passed into a `*ghaction.Client` constructed inside the validation-success branch — no earlier code path holds a live, usable client, minimizing the blast radius of a future refactor accidentally reaching the GitHub-mutating calls from an unvalidated state
- **Input Validation:** N/A (covered by 04-01 through 04-03)

## Test Implementation Guidance
**Test Type:** INTEGRATION (orchestrator-level, using a stub `ghaction`-compatible HTTP server plus a stubbed Story 2 validation result)
**Test Data Requirements:** A fake/stub validation result type with both a `Passed` and `Failed`/`Errored` case; a request-counting `httptest.Server`
**Mock/Stub Requirements:** The Story 2 validation dependency must be injectable (interface or function value) so this test can force both outcomes deterministically without actually running a build

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A validation-failure or validation-error result reaches Story 3's revert path and issues zero HTTP calls to `/git/refs` or `/git/blobs`
- [ ] A validation-success result is the only path that constructs a `*ghaction.Client` call to `CreateBranch`/`CreateCommit`
- [ ] The orchestrator's structure makes the invariant visually inspectable (single success branch), not merely enforced by a runtime flag check that a future edit could bypass

**Manual Review:**
- [ ] Code reviewed and approved

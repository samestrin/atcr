# Acceptance Criteria: New Endpoints Inherit Existing Retry, Backoff, and Redaction Behavior

**Related User Story:** [Story 4: Create a Branch and Commit the Verified Fix](../user-stories/04-create-branch-and-commit-verified-fix.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) plumbing reuse | Verifies `CreateBranch`/`CreateCommit` (and its four sub-calls) route through `postDo`/a PATCH-capable sibling — no bespoke `http.Client.Do` calls anywhere in the new code |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Reuses the exact retry-assertion pattern already proven for `CreateCheckRun` |
| Key Dependencies | `internal/ghaction.Client.postDo`'s existing exponential backoff (250ms base, 3 retries, doubling) and `redactSecrets` | No new retry/backoff implementation is written for this story |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: confirm (via code structure, not a runtime flag) that `CreateBranch` and every sub-call inside `CreateCommit` (blob/tree/commit/ref-update) call `postDo` or its PATCH-capable sibling — none construct a raw `http.Request`/`http.Client.Do` independently
- `internal/ghaction/client_test.go` - modify: add `TestCreateBranchRetriesOn5xx`, `TestCreateBranchRetriesOn429`, and `TestCreateCommitRedactsTokenInError` following the retry-assertion shape already used for `CreateCheckRun`'s coverage (a stub server that fails N times then succeeds, asserting the final call succeeds and the total request count matches `maxRetries+1`)

## Happy Path Scenarios
**Scenario 1: A transient 503 on blob creation is retried and eventually succeeds**
- **Given** a stub server that returns 503 for the first two requests to `/git/blobs` and 201 on the third
- **When** `CreateCommit` is called
- **Then** the blob-creation step retries per `postDo`'s existing exponential backoff (250ms, 500ms) and succeeds on the third attempt, and the overall `CreateCommit` call returns a `nil` error with the correct commit SHA

**Scenario 2: A 429 on the ref-update PATCH is retried identically to a POST 429**
- **Given** a stub server returns 429 for the ref-update PATCH once, then 200
- **When** `CreateCommit` reaches the ref-update step
- **Then** the same retry/backoff logic applies to the PATCH call as to the existing POST calls — confirming the PATCH-capable extension to `postDo` (added in AC 04-03) did not fork a separate, unretried code path

## Edge Cases
**Edge Case 1: Retries exhaust and the final error is a typed `APIError`**
- **Given** a stub server returns 503 for all `maxRetries+1` attempts
- **Then** `CreateBranch`/`CreateCommit` return a `*ghaction.APIError` with `StatusCode == 503` after the existing 3-retry budget is exhausted — no infinite retry loop, no panic

**Edge Case 2: A GitHub error body echoes the Authorization header**
- **Given** a stub server responds 401 with a body containing the literal bearer token string (simulating a GitHub error that echoes request context)
- **When** `CreateBranch` or any `CreateCommit` sub-call returns that error
- **Then** the returned `APIError.Message` has the token replaced with `[redacted]` via the existing `redactSecrets`/`bearerTokenPattern` — no new redaction logic is written; the new endpoints call the same `redactSecrets` method already covered for `CreateCheckRun`

## Error Conditions
**Error Scenario 1: Non-retriable 4xx (excluding 429) fails immediately without retry**
- Error message: `"github API returned 4xx: <message>"` (redacted)
- HTTP status / error code: any 4xx other than 429 — zero retries, matching existing `postDo` behavior (`resp.StatusCode >= 500 || resp.StatusCode == 429` is the only retry condition)

## Performance Requirements
- **Response Time:** Retry budget matches existing `postDo`: 3 retries, 250ms initial backoff doubling each attempt (250ms, 500ms, 1s) — no new/different backoff schedule introduced for the new endpoints
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** No new auth mechanism; `CreateBranch`/`CreateCommit` never read a token from an env var or file directly — the token flows exclusively through the existing `Client.Token` field into `postDo`'s header construction, per the story's assumption that token/repo resolution happens in the CLI layer, not inside package-level code
- **Input Validation:** Every error path for the new endpoints passes through `redactSecrets` before being returned — verified by Edge Case 2, closing the story's AC-4 requirement that "no new error path leaks a token or bypasses retry"

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A stub server with a per-endpoint failure counter (fails first N requests, then succeeds) reusable across `CreateBranch` and each `CreateCommit` sub-step; a 401 response body containing a literal token string for the redaction test
**Mock/Stub Requirements:** `httptest.NewServer`; `Client{HTTPClient: srv.Client(), Token: "tok-should-never-appear-in-error"}`

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `CreateBranch` and every `CreateCommit` sub-call retry on 5xx/429 using the existing backoff schedule, and fail immediately (no retry) on other 4xx codes
- [ ] A token echoed in an error response body is redacted in the returned `APIError.Message` for every new endpoint, not just the pre-existing check-run/comment endpoints
- [ ] No new raw `http.Client.Do` call exists anywhere in the `CreateBranch`/`CreateCommit` implementation — grep confirms all HTTP traffic routes through `postDo` or its PATCH-capable sibling

**Manual Review:**
- [ ] Code reviewed and approved

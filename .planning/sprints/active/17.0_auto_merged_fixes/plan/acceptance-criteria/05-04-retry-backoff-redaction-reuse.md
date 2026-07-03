# Acceptance Criteria: PR Endpoints Reuse Existing Retry/Backoff Plumbing and Redact Secrets From PR Content

**Related User Story:** [Story 5: Open or Update Pull Request via GitHub API](../user-stories/05-open-or-update-pull-request-via-github-api.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) plumbing reuse + orchestration-level content sanitization | Verifies `CreatePullRequest`, `UpdatePullRequest`, and the existence-check lookup route through `postDo`/`get` (no bespoke `http.Client.Do`), and that PR title/body content built from validation diagnostics is redacted before being sent |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Reuses the retry-assertion pattern already proven for `CreateCheckRun` (Story 5's reuse anchor) and Story 4's `TestCreateCommitRedactsTokenInError` |
| Key Dependencies | `internal/ghaction.Client.postDo`/`get`'s existing exponential backoff (250ms base, 3 retries, doubling) and `redactSecrets` | No new retry/backoff/redaction implementation is written for this story |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: confirm (via code structure) that `CreatePullRequest`, `UpdatePullRequest`, and `findOpenPullRequest` each call `postDo`/`get` — none construct a raw `http.Request`/`http.Client.Do` independently
- `internal/ghaction/client_test.go` - modify: add `TestCreatePullRequestRetriesOn5xx`, `TestUpdatePullRequestRetriesOn429`, and `TestCreatePullRequestRedactsTokenInError`, following the retry-assertion shape already used for `CreateCheckRun`/`CreateCommit`
- `internal/autofix/*.go` (Story 4's orchestration package) - modify: before calling `CreatePullRequest`/`UpdatePullRequest`, run any dynamic content sourced from validation stdout/stderr excerpts through the same `Client.redactSecrets`/`bearerTokenPattern` used on inbound error content so a PR title or body built from validation diagnostics cannot echo a token or credential captured during the local validation step (Story 2) — no bespoke or divergent sanitizer

## Happy Path Scenarios
**Scenario 1: A transient 503 on PR creation is retried and eventually succeeds**
- **Given** a stub server returns 503 for the first two requests to `POST /repos/{owner}/{repo}/pulls` and 201 on the third
- **When** `CreatePullRequest` is called
- **Then** the call retries per `postDo`'s existing exponential backoff (250ms, 500ms) and succeeds on the third attempt, returning the correct PR number with a `nil` error

**Scenario 2: A 429 on the update PATCH is retried identically to the existing PATCH path**
- **Given** a stub server returns 429 once for `PATCH /repos/{owner}/{repo}/pulls/{number}`, then 200
- **When** `UpdatePullRequest` is called
- **Then** the same retry/backoff logic applies as to `CreateCommit`'s ref-update PATCH (Story 4 AC 04-05) — confirming this story's new PATCH call did not fork a separate, unretried code path

**Scenario 3: PR body content derived from validation diagnostics has secrets stripped before being sent**
- **Given** a validation-step stderr excerpt intended for the PR body happens to contain a literal token string (e.g. a leaked env var echoed by a build tool)
- **When** the orchestrator builds `PullRequestRequest.Body` from that excerpt and calls `CreatePullRequest`
- **Then** the token is replaced with `[redacted]` in the outgoing request body before it is sent to GitHub — verified by inspecting the stub server's received payload, not just the error path

**Scenario 4: PR title content derived from validation diagnostics has secrets stripped before being sent**
- **Given** a validation-step summary intended for the PR title happens to contain a literal token string (e.g. a leaked env var echoed into a one-line diagnostic headline)
- **When** the orchestrator builds `PullRequestRequest.Title` from that summary and calls `CreatePullRequest`
- **Then** the token is replaced with `[redacted]` in the outgoing request title before it is sent to GitHub — verified by inspecting the stub server's received payload, symmetrically with the body redaction in Scenario 3

## Edge Cases
**Edge Case 1: Retries exhaust and the final error is a typed `APIError`**
- **Given** a stub server returns 503 for all `maxRetries+1` attempts to the pulls endpoint
- **Then** `CreatePullRequest`/`UpdatePullRequest` return a `*ghaction.APIError` with `StatusCode == 503` after the existing 3-retry budget is exhausted — no infinite retry loop, no panic

**Edge Case 2: A GitHub error body echoes the Authorization header on a PR-endpoint failure**
- **Given** a stub server responds 401 with a body containing the literal bearer token string
- **When** `CreatePullRequest`, `UpdatePullRequest`, or the existence-check lookup returns that error
- **Then** the returned `APIError.Message` has the token replaced with `[redacted]` via the existing `redactSecrets`/`bearerTokenPattern` — no new redaction logic is written; the new endpoints call the same `redactSecrets` method already covered for `CreateCheckRun`/`CreateCommit`

## Error Conditions
**Error Scenario 1: Non-retriable 4xx (excluding 429) fails immediately without retry**
- Error message: `"github API returned 4xx: <message>"` (redacted)
- HTTP status / error code: any 4xx other than 429 — zero retries, matching existing `postDo`/`get` behavior

## Performance Requirements
- **Response Time:** Retry budget matches existing `postDo`/`get`: 3 retries, 250ms initial backoff doubling each attempt (250ms, 500ms, 1s) — no new/different backoff schedule introduced for the PR endpoints
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** No new auth mechanism; `CreatePullRequest`/`UpdatePullRequest` never read a token from an env var or file directly — the token flows exclusively through the existing `Client.Token` field into `postDo`'s header construction
- **Input Validation:** Every error path for the new endpoints passes through `redactSecrets` before being returned (Edge Case 2). Additionally — and unlike the read-only `CreateCheckRun`/comment endpoints — this story's request *body* itself (PR title/body) can carry dynamic content sourced from validation diagnostics, so the same `Client.redactSecrets`/`bearerTokenPattern` used on inbound error content must also run on outbound PR title and body content, not just inbound error messages — no bespoke or divergent sanitizer — closing the story's constraint that "PR title/body content must not leak secrets even indirectly through fix diagnostics" (Scenario 3)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A stub server with a per-endpoint failure counter (fails first N requests, then succeeds) reusable across `CreatePullRequest`, `UpdatePullRequest`, and `findOpenPullRequest`; a 401 response body containing a literal token string; a `PullRequestRequest.Body` value containing a literal token string to verify outbound body redaction; a `PullRequestRequest.Title` value containing a literal token string to verify outbound title redaction
**Mock/Stub Requirements:** `httptest.NewServer`; `Client{HTTPClient: srv.Client(), Token: "tok-should-never-appear-in-request-or-error"}`; a stub handler that captures and exposes the raw received request body for the outbound-redaction assertion

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `CreatePullRequest`, `UpdatePullRequest`, and the existence-check lookup retry on 5xx/429 using the existing backoff schedule, and fail immediately (no retry) on other 4xx codes
- [ ] A token echoed in an error response body is redacted in the returned `APIError.Message` for every new endpoint
- [ ] A token or credential embedded in caller-supplied PR title/body content (sourced from validation diagnostics) is redacted before the outbound request is sent, not just in error messages
- [ ] No new raw `http.Client.Do` call exists anywhere in the `CreatePullRequest`/`UpdatePullRequest`/existence-check implementation

**Manual Review:**
- [ ] Code reviewed and approved

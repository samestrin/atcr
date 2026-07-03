# Acceptance Criteria: CreatePullRequest Opens a New PR From the Auto-Fix Branch

**Related User Story:** [Story 5: Open or Update Pull Request via GitHub API](../user-stories/05-open-or-update-pull-request-via-github-api.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) | New `CreatePullRequest` method plus a `PullRequestRequest` struct, added alongside `CreateBranch`/`CreateCommit` (Story 4) and `CreateCheckRunWithID` |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Mirrors the stub-server pattern already used for `CreateCheckRunWithID` in `client_test.go` |
| Key Dependencies | `internal/ghaction.Client.postDo` (existing) | No new HTTP transport; `POST /repos/{owner}/{repo}/pulls` is issued through `postDo` exactly like `CreateCheckRunWithID` |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: add `PullRequestRequest` struct (`Head`, `Base`, `Title`, `Body` fields) and `CreatePullRequest(ctx context.Context, owner, repo string, req PullRequestRequest) (int, error)`, following `CreateCheckRunWithID`'s shape (build a body map, `postDo` into a typed response struct, return the decoded field)
- `internal/ghaction/client_test.go` - modify: add `TestCreatePullRequestSuccess` and `TestCreatePullRequestReturnsNumber` asserting the request payload (`head`, `base`, `title`, `body`) and that the decoded `number` field is returned
- `internal/autofix/*.go` (Story 4's orchestration package) - modify: call `CreatePullRequest` as the terminal step after `Client.CreateCommit` succeeds, passing the branch created in Story 4

## Happy Path Scenarios
**Scenario 1: A new PR is opened for a freshly pushed auto-fix branch**
- **Given** Story 4 has pushed branch `atcr-autofix/2026-07-02-abc123` with a verified commit, and no open PR exists for that branch
- **When** `Client.CreatePullRequest(ctx, owner, repo, PullRequestRequest{Head: "atcr-autofix/2026-07-02-abc123", Base: "main", Title: "atcr: auto-fix TD-042", Body: "..."})` is called
- **Then** a `POST /repos/{owner}/{repo}/pulls` request is sent with `head`, `base`, `title`, and `body` in the JSON payload, and the method returns the GitHub-assigned PR number decoded from the response with a `nil` error

**Scenario 2: The PR title/body identify the fix being proposed**
- **Given** a fix targeting a specific finding (e.g. a technical-debt item or flagged issue)
- **When** the orchestrator builds the `PullRequestRequest` for that fix
- **Then** the `Title` and `Body` are populated with content identifying the fix (e.g. referencing the finding or TD item), so a human reviewer can tell what the PR addresses without opening the diff first

## Edge Cases
**Edge Case 1: Base branch does not exist or head has no commits between it and base**
- **Given** GitHub returns 422 for an invalid `base` (nonexistent branch) or a `head` with nothing to compare (empty diff)
- **When** `CreatePullRequest` is called
- **Then** the method returns a `*ghaction.APIError` with `StatusCode == 422` and a redacted message identifying the failure, without panicking or retrying (422 is not a retriable status per `postDo`'s existing retry predicate)

**Edge Case 2: A duplicate-open-PR 422 is returned even though the caller believed no PR existed**
- **Given** a race where another process opened a PR for the same head between the existence check (AC 05-02) and this call
- **When** `CreatePullRequest` returns GitHub's "A pull request already exists" 422
- **Then** the error surfaces as a typed `*ghaction.APIError` (StatusCode 422) so the caller can distinguish this from a systemic failure and treat it as a non-fatal race, not a crash

## Error Conditions
**Error Scenario 1: Transient 5xx/429 during PR creation**
- Error message: none — retried transparently up to 3 times via `postDo`'s existing 250ms-doubling backoff, matching Story 4's `CreateBranch`/`CreateCommit` behavior
- HTTP status / error code: 500-599 or 429 (retried); any other non-2xx returns immediately

**Error Scenario 2: Invalid or expired token**
- Error message: `"github API returned 401: <redacted message>"`
- HTTP status / error code: 401 — no retry, propagated as `*ghaction.APIError`

## Performance Requirements
- **Response Time:** Single `postDo` round-trip (no chained calls, unlike `CreateCommit`'s blob/tree/commit/ref sequence); completes within `postDo`'s existing per-attempt timeout (default 90s via `httpClient()`)
- **Throughput:** N/A — one PR create per `--auto-fix` run

## Security Considerations
- **Authentication/Authorization:** Uses the existing `Client.Token` (Bearer auth) already required for `CreateBranch`/`CreateCommit`; no new token or scope beyond the `contents:write`/`repo` scope Story 4 already documents, since opening a PR requires no additional GitHub scope
- **Input Validation:** `Title`/`Body` content must not embed raw secrets — validated in AC 05-04. `Head`/`Base` are branch-name strings validated by the GitHub API itself (a malformed ref name returns 422, not a client-side crash)

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A stub `httptest.Server` handling `POST /repos/{owner}/{repo}/pulls`, returning `{"number": 42}` on success and a 422 body (`{"message": "..."}`) for the collision/invalid-base edge cases
**Mock/Stub Requirements:** `httptest.NewServer`; `Client{HTTPClient: srv.Client(), APIURL: srv.URL, Token: "test-token"}`; no GitHub API dependency in tests

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `CreatePullRequest` sends `head`, `base`, `title`, `body` and returns the decoded PR number
- [ ] A 422 (invalid base, duplicate PR) surfaces as a typed `*ghaction.APIError`, not a generic error or panic
- [ ] No raw `http.Client.Do` call exists in the new code — all traffic routes through `postDo`

**Manual Review:**
- [ ] Code reviewed and approved

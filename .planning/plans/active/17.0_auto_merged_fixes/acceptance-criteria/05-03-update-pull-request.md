# Acceptance Criteria: UpdatePullRequest Refreshes an Existing Open PR

**Related User Story:** [Story 5: Open or Update Pull Request via GitHub API](../user-stories/05-open-or-update-pull-request-via-github-api.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) | New `UpdatePullRequest` method issuing `PATCH /repos/{owner}/{repo}/pulls/{number}`, reusing the same `PullRequestRequest` struct as `CreatePullRequest` (AC 05-01) |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Extends `postDo` (or its PATCH-capable sibling, already introduced for `CreateCommit`'s ref update in Story 4 AC 04-03) to the PR endpoint |
| Key Dependencies | `internal/ghaction.Client.postDo`'s PATCH-capable sibling (added in Story 4) | No new HTTP verb plumbing — this story reuses the PATCH path Story 4 already added for ref updates |

## Related Files
- `internal/ghaction/client.go` - modify: add `UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, req PullRequestRequest) error`, sending `title`/`body` (and `base` if changed) via `PATCH /repos/{owner}/{repo}/pulls/{prNumber}` through the same PATCH-capable `postDo` sibling used by `CreateCommit`'s ref-update step
- `internal/ghaction/client_test.go` - modify: add `TestUpdatePullRequestSuccess` and `TestUpdatePullRequestNotFound` asserting the PATCH payload and 404 handling
- `internal/autofix/*.go` (Story 4's orchestration package) - modify: call `UpdatePullRequest` with the existence-check's PR number (AC 05-02) when an open PR is found for the branch

## Happy Path Scenarios
**Scenario 1: A re-run on the same branch refreshes the existing PR's title/body**
- **Given** an open PR #17 already exists for branch `atcr-autofix/abc123`, and the maintainer re-runs `--auto-fix` after the fix's diagnostics changed slightly (e.g. a re-validated commit was force-pushed to the same branch by Story 4)
- **When** `Client.UpdatePullRequest(ctx, owner, repo, 17, PullRequestRequest{Title: "atcr: auto-fix TD-042 (updated)", Body: "..."})` is called
- **Then** a `PATCH /repos/{owner}/{repo}/pulls/17` request is sent with the refreshed `title`/`body`, and the method returns `nil` on a 200 response — no new PR object is created

**Scenario 2: Update is idempotent across repeated identical calls**
- **Given** PR #17 already carries the exact title/body content being submitted
- **When** `UpdatePullRequest` is called again with unchanged content
- **Then** GitHub accepts the PATCH as a no-op content change and the method still returns `nil` — repeated `--auto-fix` runs against a stable fix never error out on the update path

## Edge Cases
**Edge Case 1: The looked-up PR number no longer exists or was closed between the existence check and the update call**
- **Given** a race where PR #17 is closed/merged by a human between AC 05-02's lookup and this call
- **When** `UpdatePullRequest` is called with the now-stale PR number
- **Then** GitHub returns 404 (or 422 for a closed PR that rejects certain field updates), and the method returns a typed `*ghaction.APIError` rather than silently succeeding or panicking — the caller can surface this as "PR #17 was closed externally; a new PR was not created automatically" rather than masking the discrepancy

**Edge Case 2: Only a subset of fields need updating (e.g. body only, title unchanged)**
- **Given** the orchestrator only wants to refresh the PR body (not the title)
- **When** `PullRequestRequest.Title` is left as the zero value
- **Then** the PATCH payload omits (or explicitly resends the previous value for) fields not being changed, per GitHub's PATCH semantics where an omitted field is left untouched — this AC documents that `UpdatePullRequest` always sends both `title` and `body` from the caller-supplied `req` (the orchestrator is responsible for populating both with current values), not partial-field diffing inside the client

## Error Conditions
**Error Scenario 1: PR not found (closed/deleted between lookup and update)**
- Error message: `"github API returned 404: <redacted>"`
- HTTP status / error code: 404 — no retry (not in `postDo`'s retriable set), propagated as `*ghaction.APIError`

**Error Scenario 2: Transient 5xx/429 during update**
- Error message: none — retried transparently via the existing PATCH-capable `postDo` sibling's backoff, identical to Story 4's ref-update retry coverage
- HTTP status / error code: 500-599 or 429 (retried up to 3 times)

## Performance Requirements
- **Response Time:** Single PATCH round-trip; completes within the same per-attempt timeout as `CreatePullRequest`
- **Throughput:** N/A — at most one update per `--auto-fix` re-run

## Security Considerations
- **Authentication/Authorization:** Same `Client.Token`/scope as `CreatePullRequest`; no additional GitHub permission required to update a PR opened under the same token's identity
- **Input Validation:** `Title`/`Body` pass through the same secret-redaction path validated in AC 05-04 before being sent, so a re-run cannot leak a token into an already-public PR any more than the initial create could

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A stub server handling `PATCH /repos/{owner}/{repo}/pulls/{number}`, returning 200 for a valid number, 404 for an unknown/closed number
**Mock/Stub Requirements:** `httptest.NewServer`; `Client{HTTPClient: srv.Client(), APIURL: srv.URL, Token: "test-token"}`; reuse of the PATCH-capable `postDo` sibling under test already exercised by Story 4's `TestCreateCommitRedactsTokenInError`-style coverage

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `UpdatePullRequest` sends `title`/`body` via `PATCH /repos/{owner}/{repo}/pulls/{prNumber}` and returns `nil` on success
- [ ] A 404 (stale/closed PR number) surfaces as a typed `*ghaction.APIError`, not a silent success or panic
- [ ] Repeated calls with identical content are idempotent (no error on a no-op update)

**Manual Review:**
- [ ] Code reviewed and approved

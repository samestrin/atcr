# Acceptance Criteria: Existence Check Decides Create-vs-Update and Avoids Duplicate PRs

**Related User Story:** [Story 5: Open or Update Pull Request via GitHub API](../user-stories/05-open-or-update-pull-request-via-github-api.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) + orchestration decision | `findOpenPullRequest` (or small exported lookup) via `GET /repos/{owner}/{repo}/pulls?head={owner}:{branch}&state=open`, called from the `internal/autofix` orchestrator before choosing create vs. update |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Reuses `Client.get`'s existing stub-server pattern |
| Key Dependencies | `internal/ghaction.Client.get` (existing) | Lookup is a GET, so it reuses `get`, not `postDo` |

## Related Files
- `internal/ghaction/client.go` - modify: add `findOpenPullRequest(ctx context.Context, owner, repo, branch string) (int, bool, error)` (or equivalent exported lookup) that calls `GET /repos/{owner}/{repo}/pulls?head={owner}:{branch}&state=open` via `c.get`, decoding a JSON array and returning the first match's number and `found=true`, or `found=false` with no error when the array is empty
- `internal/ghaction/client_test.go` - modify: add `TestFindOpenPullRequestFound` and `TestFindOpenPullRequestNotFound` asserting the query string (`head=owner:branch&state=open`) and the found/not-found decoding
- `internal/autofix/*.go` (Story 4's orchestration package) - modify: after `CreateCommit` succeeds, call the existence-check lookup first; branch to `CreatePullRequest` (AC 05-01) when no open PR is found, or `UpdatePullRequest` (AC 05-03) when one is found

## Happy Path Scenarios
**Scenario 1: No open PR exists for the branch — orchestrator creates one**
- **Given** `GET /repos/{owner}/{repo}/pulls?head=owner:atcr-autofix/abc123&state=open` returns an empty JSON array
- **When** the `--auto-fix` orchestrator runs the existence check after Story 4's commit succeeds
- **Then** the lookup returns `found=false`, and the orchestrator calls `CreatePullRequest` (not `UpdatePullRequest`)

**Scenario 2: An open PR already exists for the branch — orchestrator updates it instead**
- **Given** `GET /repos/{owner}/{repo}/pulls?head=owner:atcr-autofix/abc123&state=open` returns a one-element array with `{"number": 17, ...}` (e.g. a second `--auto-fix` run on the same branch after amending the fix)
- **When** the orchestrator runs the existence check
- **Then** the lookup returns `found=true, number=17`, and the orchestrator calls `UpdatePullRequest(ctx, owner, repo, 17, req)` instead of `CreatePullRequest` — no duplicate PR is created

## Edge Cases
**Edge Case 1: Multiple open PRs unexpectedly match the same head (manual PR created outside atcr)**
- **Given** the lookup returns more than one open PR for the same `head` (e.g. a human manually opened a second PR against the same branch)
- **When** the orchestrator processes the result
- **Then** it deterministically picks the first (or lowest-numbered) match and updates that one, logging a warning that more than one open PR was found for the branch — it does not create a third PR and does not error out

**Edge Case 2: The existence-check GET itself fails transiently**
- **Given** the lookup GET returns 503 twice then 200 with an empty array
- **When** the existence check runs
- **Then** `c.get`'s existing retry/backoff transparently retries and the orchestrator proceeds to `CreatePullRequest` once the lookup succeeds — no separate retry logic is written for the existence check

## Error Conditions
**Error Scenario 1: Existence check exhausts retries**
- Error message: `"github API /repos/{owner}/{repo}/pulls returned 503: <redacted>"`
- HTTP status / error code: 503 (or 429) after 3 retries — the orchestrator treats this as a hard failure for the run (it must not guess create-vs-update and risk a duplicate PR) and surfaces a clear error naming the branch, per the story's stranded-branch mitigation

**Error Scenario 2: Malformed `head` query parameter (invalid branch name)**
- Error message: `"github API returned 422: <redacted>"`
- HTTP status / error code: 422 — propagated as a typed error; this should not occur in practice since the branch name originates from Story 4's own successful `CreateBranch` call

## Performance Requirements
- **Response Time:** Single GET round-trip before the create/update decision; adds at most one extra request to the `--auto-fix` flow's per-run budget
- **Throughput:** N/A — one existence check per `--auto-fix` run

## Security Considerations
- **Authentication/Authorization:** Uses the same `Client.Token`; the `pulls` list endpoint requires only read access, already covered by the token scope Story 4 documents
- **Input Validation:** `branch` is URL-query-escaped before being embedded in the `head=owner:branch` query parameter so a branch name containing special characters cannot corrupt the request path or inject extra query parameters

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Stub responses for empty array, single-match array, and multi-match array on `GET /repos/{owner}/{repo}/pulls`; a fake `internal/autofix` orchestration step (or table-driven test at the `Client` boundary) asserting `CreatePullRequest` vs. `UpdatePullRequest` is called based on the lookup result
**Mock/Stub Requirements:** `httptest.NewServer` returning the query-string-dependent bodies above; assertions on which of `CreatePullRequest`/`UpdatePullRequest` fired (e.g. via a call-counting test double or by asserting on the HTTP method/path sequence hitting the stub server: GET then POST, or GET then PATCH)

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] The existence-check lookup queries `GET /repos/{owner}/{repo}/pulls?head={owner}:{branch}&state=open` and correctly reports found/not-found
- [ ] A second `--auto-fix` run against a branch with an already-open PR calls `UpdatePullRequest`, never `CreatePullRequest` — verified end to end, closing the story's "100% of successful runs result in exactly one open PR per branch" success criterion
- [ ] Existence-check retry/backoff reuses `c.get` — no bespoke retry loop

**Manual Review:**
- [ ] Code reviewed and approved

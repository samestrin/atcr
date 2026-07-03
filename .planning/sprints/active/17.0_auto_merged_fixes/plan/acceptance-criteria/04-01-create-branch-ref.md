# Acceptance Criteria: CreateBranch Creates a New Git Ref at a Base SHA

**Related User Story:** [Story 4: Create a Branch and Commit the Verified Fix](../user-stories/04-create-branch-and-commit-verified-fix.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) | Extends the existing `Client` struct; no new struct or client type |
| Test Framework | Go `testing` + `net/http/httptest` + `testify/require`/`assert` | Mirrors the existing pattern in `internal/ghaction/client_test.go` |
| Key Dependencies | `internal/ghaction.Client.postDo` (existing), GitHub REST "Git Data API" `POST /repos/{owner}/{repo}/git/refs` | No new HTTP client, no new third-party dependency |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: add `CreateBranch(ctx context.Context, owner, repo, branch, sha string) error`, calling `c.postDo(ctx, fmt.Sprintf("/repos/%s/%s/git/refs", owner, repo), map[string]any{"ref": "refs/heads/"+branch, "sha": sha}, nil)`
- `internal/ghaction/client_test.go` - modify: add `TestCreateBranch*` cases using an `httptest.Server` stub, following the existing `TestCreateCheckRun` pattern (assert path, auth header, and request body)

## Happy Path Scenarios
**Scenario 1: Branch created successfully from a valid base SHA**
- **Given** a `Client` configured with a valid token and API URL, and a base SHA that exists on `owner/repo`
- **When** `CreateBranch(ctx, owner, repo, "atcr-fix/2026-07-02-1", sha)` is called
- **Then** the client issues `POST /repos/{owner}/{repo}/git/refs` with body `{"ref": "refs/heads/atcr-fix/2026-07-02-1", "sha": sha}`, the call returns `nil` error on a 2xx response, and the `Authorization: Bearer <token>` header is present exactly as on existing `postDo` calls

**Scenario 2: Branch name is passed without a `refs/heads/` prefix**
- **Given** a caller supplies a bare branch name (e.g. `"atcr-fix/abc123"`, no `refs/` prefix)
- **When** `CreateBranch` is called
- **Then** the method itself prepends `refs/heads/` before sending the request — callers never need to know the ref-naming convention

## Edge Cases
**Edge Case 1: Branch name contains characters requiring no extra escaping in a JSON body**
- **Given/When** a branch name with `/`, `-`, and alphanumerics (the plan's stated deterministic-prefix-plus-timestamp scheme) is passed
- **Then** the JSON body encodes it verbatim (no URL escaping needed since it travels in the request body, not the path) and GitHub receives the exact ref string

**Edge Case 2: Context cancellation during the request**
- **Given** a context that is cancelled mid-flight
- **When** `CreateBranch` is called
- **Then** the call returns the context's error (inherited from `postDo`'s existing `sleepCtx`/request-building behavior) and issues no partial or duplicate ref-creation call

## Error Conditions
**Error Scenario 1: Base SHA does not exist on the repository**
- Error message: surfaced via `*ghaction.APIError` with GitHub's own message (e.g. `"github API returned 422: Object does not exist"`)
- HTTP status / error code: 422

**Error Scenario 2: Ref already exists (branch name collision)**
- Error message: `"github API returned 422: Reference already exists"` (see AC 04-02 for the caller-facing collision-handling contract)
- HTTP status / error code: 422 — handled as a non-retriable `*ghaction.APIError`, not retried by `postDo` (only 5xx/429 retry)

**Error Scenario 3: Insufficient token scope**
- Error message: `"github API returned 403: Resource not accessible by integration"` (or equivalent GitHub 403 body)
- HTTP status / error code: 403

## Performance Requirements
- **Response Time:** Single round trip under normal conditions; retried up to 3 times with exponential backoff (250ms base, per existing `postDo`) only on 5xx/429 — matches existing client behavior, no new SLA
- **Throughput:** One `CreateBranch` call per auto-fix run; no batching requirement

## Security Considerations
- **Authentication/Authorization:** Reuses `postDo`'s existing `Authorization: Bearer <token>` header construction; `CreateBranch` never constructs its own HTTP request
- **Input Validation:** Branch name and SHA are passed through as opaque strings to the JSON body (no shell interpolation, no local `git` invocation) — GitHub's API is the sole validator of ref-name legality and SHA existence

## Test Implementation Guidance
**Test Type:** UNIT (via `httptest.Server`, matching existing `internal/ghaction` test style — no live GitHub calls)
**Test Data Requirements:** A stub server asserting request method (`POST`), path (`/repos/{owner}/{repo}/git/refs`), and JSON body (`ref`, `sha` keys); a 201 response for the happy path, a 422 response body for the collision/invalid-SHA cases
**Mock/Stub Requirements:** `httptest.NewServer` with `Client.APIURL` and `Client.HTTPClient` pointed at it, exactly as `TestCreateCheckRunAllowsLoopbackHTTP` and `TestCreateCheckRun` already do

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `CreateBranch` issues exactly one `POST /repos/{owner}/{repo}/git/refs` call with the correct `ref`/`sha` body
- [ ] A 2xx response yields a `nil` error; a non-2xx response yields a populated `*ghaction.APIError` with the redacted GitHub message
- [ ] Branch name is normalized to `refs/heads/<branch>` inside the method, not by the caller

**Manual Review:**
- [ ] Code reviewed and approved

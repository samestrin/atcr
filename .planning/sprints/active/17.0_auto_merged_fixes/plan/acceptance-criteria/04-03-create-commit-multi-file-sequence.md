# Acceptance Criteria: CreateCommit Builds a Multi-File Commit via Blob/Tree/Commit/Ref Sequence

**Related User Story:** [Story 4: Create a Branch and Commit the Verified Fix](../user-stories/04-create-branch-and-commit-verified-fix.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) + new `CommitRequest`/`CommitFile` types | New request struct added to `internal/ghaction` alongside `CheckRunRequest`; no ORM, no local `git` shell-out |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Stub server must route five distinct method+path endpoints (read-commit `GET /git/commits/{sha}`, `POST` blobs, trees, commits, `PATCH` refs) and assert call order |
| Key Dependencies | GitHub Git Data API: `GET .../git/commits/{parent_sha}` (to resolve the parent commit's tree SHA), `POST .../git/blobs`, `POST .../git/trees`, `POST .../git/commits`, `PATCH .../git/refs/heads/{branch}` — all via existing `postDo`/a GET sibling | The parent SHA supplied by Story 4 is the default-branch HEAD, which is a COMMIT SHA (not a tree SHA), so `base_tree` must be resolved from it first; `PATCH` requires `postDo` (or a sibling) to support a configurable HTTP method; see Related Files |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: add `CommitRequest` (branch, message, `ParentSHA` — the PARENT COMMIT SHA, not a pre-resolved tree SHA — and `Files []CommitFile`) and `CommitFile` (path, content, `Deleted bool`) types; add `CreateCommit(ctx context.Context, owner, repo string, req CommitRequest) (string, error)` orchestrating read-commit → blob → tree → commit → ref-update calls (it first `GET /git/commits/{ParentSHA}` to read `tree.sha`, then uses that as `base_tree`), returning the new commit SHA
- `internal/ghaction/client.go` - modify: `postDo` currently hardcodes `http.MethodPost`; extend it (or add a small `patchDo`/method parameter) so the final ref-update step (`PATCH /git/refs/heads/{branch}`) reuses the same auth/retry/redaction plumbing rather than a bespoke request builder
- `internal/ghaction/client_test.go` - modify: add `TestCreateCommit*` cases with a stub server asserting the calls occur in order (GET commit → blob(s) → tree → commit → ref PATCH) and that the tree's `base_tree` is the `tree.sha` read from the parent commit's `GET /git/commits/{parent_sha}` response — not the parent commit SHA itself

## Happy Path Scenarios
**Scenario 1: Single-file change produces a five-call sequence**
- **Given** a `CommitRequest` with one changed file (path + new content), a branch name, a commit message, and a `ParentSHA` that is a COMMIT SHA (the default-branch HEAD supplied by Story 4)
- **When** `CreateCommit(ctx, owner, repo, req)` is called
- **Then** the client: (1) GETs `/git/commits/{ParentSHA}` and reads its `tree.sha` to resolve the base tree, (2) POSTs the file content to `/git/blobs` and receives a blob SHA, (3) POSTs a tree to `/git/trees` referencing `base_tree` (the resolved parent tree SHA) plus one entry for the changed path/blob SHA, (4) POSTs a commit to `/git/commits` with the new tree SHA and `ParentSHA` as the parent, (5) PATCHes `/git/refs/heads/{branch}` with the new commit SHA — and the method returns that commit SHA with a `nil` error

**Scenario 2: Multi-file change is expressed as one atomic commit**
- **Given** a `CommitRequest` with three changed files (mirroring Story 1's patch apply touching multiple files)
- **When** `CreateCommit` is called
- **Then** exactly one read-commit `GET` occurs first to resolve the base tree, followed by exactly three blob-creation calls (one per file), followed by exactly one tree-creation call whose entries array contains all three paths, followed by exactly one commit call and one ref-update call — never one commit per file

**Scenario 3: A deleted file is expressed via a tree entry with a null SHA**
- **Given** a `CommitFile` with `Deleted: true`
- **When** the tree is constructed
- **Then** no blob is created for that file, and the tree entry for its path carries `sha: null` (GitHub's documented mechanism for removing a path from a tree), leaving the file absent from the resulting commit

## Edge Cases
**Edge Case 1: Empty file list**
- **Given** a `CommitRequest.Files` is empty
- **Then** `CreateCommit` returns an error before making any HTTP call (a commit with no changes is a caller bug, not something to silently send to GitHub as a no-op tree)

**Edge Case 2: Partial-sequence failure leaves no orphaned branch state**
- **Given** blob and tree creation succeed but commit creation fails (e.g. 422 due to a stale parent SHA)
- **When** `CreateCommit` returns
- **Then** the branch ref is never PATCHed (the ref-update call is only reached after a successful commit-creation response), so the branch still points at its prior valid commit — no partial/broken ref state, matching the story's risk mitigation that orphaned blobs/trees are inert and GitHub garbage-collects them

**Edge Case 3: Large file content**
- **Given** a changed file's content is large (e.g. approaching GitHub's blob size limits)
- **Then** `CreateCommit` does not attempt local chunking or compression — it passes the content through to the blob-creation call as-is and surfaces GitHub's own size-limit error if rejected (no new size-validation logic is invented client-side)

## Error Conditions
**Error Scenario 1: Blob creation fails partway through a multi-file set**
- Error message: `"github API returned <code>: <github message>"`, wrapped with which step failed (e.g. `"creating blob for path %q: %w"`) so a human reviewing a failed auto-fix run knows exactly where the sequence stopped
- HTTP status / error code: propagated from the failing call (e.g. 422, 403, 500-after-retries-exhausted)

**Error Scenario 2: Tree creation references an invalid base tree SHA**
- Error message: `"creating tree: github API returned 422: ..."`
- HTTP status / error code: 422

**Error Scenario 3: Ref update fails after a successful commit object was created**
- Error message: `"updating ref refs/heads/%s: github API returned <code>: ..."` — the returned error must make clear the commit object exists on GitHub but the branch was not advanced to it, per the story's risk table (Medium risk: "tree creation succeeds but ref update fails")
- HTTP status / error code: propagated from the PATCH call

## Performance Requirements
- **Response Time:** Sequential (not parallel) calls are acceptable — blob creation for N files, then tree, then commit, then ref update is O(N) round trips; this matches GitHub's Git Data API design and is not a hot path (one commit per auto-fix run)
- **Throughput:** N/A — single orchestrated call per invocation, no batching across multiple findings in one `CreateCommit` call

## Security Considerations
- **Authentication/Authorization:** All four calls reuse `postDo`'s (or its PATCH-capable sibling's) existing auth header construction — no bespoke request building that could skip the `Authorization` header
- **Input Validation:** File paths and content are passed through as opaque data to GitHub's API; no local filesystem access occurs inside `CreateCommit` (it operates purely on the `CommitRequest` payload the caller already assembled from the validated working tree)

## Test Implementation Guidance
**Test Type:** UNIT (via `httptest.Server` routing on method+path to simulate all five Git Data API endpoints)
**Test Data Requirements:** A stub server with a `ServeMux`-style dispatch on `GET /git/commits/{sha}` (returning a canned `tree.sha`), `POST /git/blobs`, `POST /git/trees`, `POST /git/commits`, `PATCH /git/refs/heads/{branch}` returning canned SHAs; a call-order recorder (slice of endpoint names appended per request) to assert the `GET commit → blob(s) → tree → commit → ref` sequencing, and an assertion that the POSTed tree's `base_tree` equals the `tree.sha` from the GET response
**Mock/Stub Requirements:** Single `httptest.Server`; no real GitHub calls; a helper to build a minimal valid `CommitRequest` for reuse across sub-tests

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] Read-commit `GET` → blob → tree → commit → ref-update calls occur in that exact order for both single- and multi-file `CommitRequest`s, and the tree's `base_tree` is the `tree.sha` resolved from the parent commit's GET response (not the `ParentSHA` commit SHA)
- [x] A deleted file produces a null-SHA tree entry, not a blob-creation call
- [x] A failure at any step short-circuits the remaining steps (no ref update after a failed commit; no commit after a failed tree)
- [x] The returned commit SHA matches the SHA from the commit-creation response, not the ref-update response

**Manual Review:**
- [x] Code reviewed and approved

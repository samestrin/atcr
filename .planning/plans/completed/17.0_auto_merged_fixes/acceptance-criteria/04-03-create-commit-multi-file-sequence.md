# Acceptance Criteria: CreateCommit Builds a Multi-File Commit via Blob/Tree/Commit/Ref Sequence

**Related User Story:** [Story 4: Create a Branch and Commit the Verified Fix](../user-stories/04-create-branch-and-commit-verified-fix.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package method (`internal/ghaction.Client`) + new `CommitRequest`/`CommitFile` types | New request struct added to `internal/ghaction` alongside `CheckRunRequest`; no ORM, no local `git` shell-out |
| Test Framework | Go `testing` + `net/http/httptest` + `testify` | Stub server must route four distinct endpoints (blobs, trees, commits, refs) and assert call order |
| Key Dependencies | GitHub Git Data API: `POST .../git/blobs`, `POST .../git/trees`, `POST .../git/commits`, `PATCH .../git/refs/heads/{branch}` — all via existing `postDo` | `PATCH` requires `postDo` (or a sibling) to support a configurable HTTP method; see Related Files |

### Related Files (from codebase-discovery.json)
- `internal/ghaction/client.go` - modify: add `CommitRequest` (branch, message, base tree/parent SHA, `Files []CommitFile`) and `CommitFile` (path, content, `Deleted bool`) types; add `CreateCommit(ctx context.Context, owner, repo string, req CommitRequest) (string, error)` orchestrating blob → tree → commit → ref-update calls, returning the new commit SHA
- `internal/ghaction/client.go` - modify: `postDo` currently hardcodes `http.MethodPost`; extend it (or add a small `patchDo`/method parameter) so the final ref-update step (`PATCH /git/refs/heads/{branch}`) reuses the same auth/retry/redaction plumbing rather than a bespoke request builder
- `internal/ghaction/client_test.go` - modify: add `TestCreateCommit*` cases with a stub server asserting the four calls occur in order (blob(s) → tree → commit → ref PATCH) and that the tree references the correct base tree SHA

## Happy Path Scenarios
**Scenario 1: Single-file change produces a four-call sequence**
- **Given** a `CommitRequest` with one changed file (path + new content), a branch name, a commit message, and a base/parent commit SHA
- **When** `CreateCommit(ctx, owner, repo, req)` is called
- **Then** the client: (1) POSTs the file content to `/git/blobs` and receives a blob SHA, (2) POSTs a tree to `/git/trees` referencing `base_tree` (the parent commit's tree) plus one entry for the changed path/blob SHA, (3) POSTs a commit to `/git/commits` with the new tree SHA and the parent commit SHA, (4) PATCHes `/git/refs/heads/{branch}` with the new commit SHA — and the method returns that commit SHA with a `nil` error

**Scenario 2: Multi-file change is expressed as one atomic commit**
- **Given** a `CommitRequest` with three changed files (mirroring Story 1's patch apply touching multiple files)
- **When** `CreateCommit` is called
- **Then** exactly three blob-creation calls occur (one per file), followed by exactly one tree-creation call whose entries array contains all three paths, followed by exactly one commit call and one ref-update call — never one commit per file

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
**Test Type:** UNIT (via `httptest.Server` routing on method+path to simulate all four Git Data API endpoints)
**Test Data Requirements:** A stub server with a `ServeMux`-style dispatch on `/git/blobs`, `/git/trees`, `/git/commits`, `/git/refs/heads/{branch}` returning canned SHAs; a call-order recorder (slice of endpoint names appended per request) to assert sequencing
**Mock/Stub Requirements:** Single `httptest.Server`; no real GitHub calls; a helper to build a minimal valid `CommitRequest` for reuse across sub-tests

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Blob → tree → commit → ref-update calls occur in that exact order for both single- and multi-file `CommitRequest`s
- [ ] A deleted file produces a null-SHA tree entry, not a blob-creation call
- [ ] A failure at any step short-circuits the remaining steps (no ref update after a failed commit; no commit after a failed tree)
- [ ] The returned commit SHA matches the SHA from the commit-creation response, not the ref-update response

**Manual Review:**
- [ ] Code reviewed and approved

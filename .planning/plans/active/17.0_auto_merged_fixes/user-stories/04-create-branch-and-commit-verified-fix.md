# User Story 4: Create a Branch and Commit the Verified Fix

**Plan:** [17.0: Auto-Merged Fixes Execution](../plan.md)

## User Story

**As an** ATCR maintainer running `atcr` with the opt-in `--auto-fix` flow
**I want** ATCR to push a locally-validated fix to a new branch via the GitHub API once — and only once — local validation has passed
**So that** the verified fix becomes visible on GitHub for review without ATCR ever pushing an unvalidated or broken change, and without a second HTTP client duplicating auth, retry, and redaction logic that already exists

## Story Context

- **Background:** This story is the pivot point of the plan: everything before it (Story 1's patch apply, Story 2's validation, Story 3's revert) is entirely local and fully undoable. This story is the first step that mutates GitHub state, and once it runs, Story 3's revert can no longer undo it — a pushed branch/commit is a one-way door (per plan Risk Mitigation). The reuse anchor is `internal/ghaction/client.go`'s `Client` and its `postDo`/`get` helpers, which already provide retry/backoff on 5xx/429, a typed `APIError`, and secret redaction (`redactSecrets`). Today's only consumer of that client, `cmd/atcr/github.go`'s `newGithubCmd`, posts a check run and inline comments to an *already-existing* PR — it has no branch- or commit-creation surface, confirming this story is genuinely new code, not a rewire of existing behavior.
- **Assumptions:**
  - Story 2 (configurable local validation) has already run and returned success for the exact working-tree state this story commits — this story is never invoked as a first step and never invoked after a validation failure.
  - The working tree at commit time reflects Story 1's applied patch (and has *not* been reverted by Story 3, which only fires on validation failure).
  - A GitHub token with `contents:write` (or classic `repo` scope) and the target `owner/repo` are already resolved by the CLI layer (mirroring the existing `--repo`/`--token`/`GITHUB_REPOSITORY`/`GITHUB_TOKEN` resolution in `cmd/atcr/github.go`) — this story's package-level code receives them as parameters, it does not read env vars itself.
  - GitHub's Git Data API (blobs → tree → commit → ref) is the mechanism, not local `git` shell-outs and not the simpler single-file Contents API — a patch can touch multiple files in one atomic commit, which the Contents API cannot express in a single call.
- **Constraints:**
  - New methods are added to the existing `internal/ghaction.Client` (`CreateBranch`, `CreateCommit`) — no second HTTP client, no bypassing `postDo`'s retry/backoff/redaction plumbing.
  - No remote mutation happens until Story 2's validation has already succeeded; this story's entry point must be unreachable from any earlier stage of the flow.
  - The branch name and base SHA must be resolved deterministically (e.g. a fixed prefix plus a timestamp/identifier and the current default-branch HEAD SHA at invocation time) so retried or concurrent auto-fix runs do not collide on an existing ref.
  - Out of scope: opening or updating the pull request itself (AC5's other half — Story 5), and any local revert of a commit already pushed by this story (Story 3's revert is working-tree-only and cannot reach GitHub state).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Story 1 (Apply a Parsed Patch to the Working Tree Without Corruption) — supplies the validated file changes to commit; Story 2 (Run a Configurable Local Validation Step) — its passing result is the sole gate that allows this story's methods to be invoked |

## Success Criteria (SMART Format)

- **Specific:** Given a set of locally-applied and locally-validated file changes, `internal/ghaction.Client.CreateBranch` creates a new ref on `owner/repo` from a specified base SHA, and `CreateCommit` builds the blob/tree/commit chain for the changed files and advances that branch's ref to the new commit SHA.
- **Measurable:** 100% of integration tests against a stubbed GitHub API (httptest server, mirroring the existing `internal/ghaction` test pattern) confirm the correct sequence of calls (blob creation per changed file → tree creation → commit creation → ref update) and that no ref-creation or commit call is ever issued when validation status is failure; retry/backoff and redaction behavior for the new endpoints matches the existing `postDo`/`get` coverage.
- **Achievable:** The HTTP plumbing (auth headers, retry on 5xx/429, `APIError`, secret redaction) is fully inherited from the existing `Client` — this story adds new REST calls and response-shape parsing, not new transport logic.
- **Relevant:** This is the first point in the plan where a verified fix becomes visible outside the local machine; without it, PLAN_GOAL's "push a branch" half of the flow has no implementation for Story 5 (PR open/update) to build on.
- **Time-bound:** Deliverable within this plan's sprint cycle, sequenced strictly after Story 1 and Story 2 land, since this story's entry point is only reachable behind a passing validation result.

## Acceptance Criteria Overview

1. `Client.CreateBranch(ctx, owner, repo, branch, sha)` creates a new Git ref (`refs/heads/<branch>`) at the given base SHA, returning a clear error (surfaced via the existing `APIError` type) if the ref already exists or the base SHA is invalid.
2. `Client.CreateCommit(ctx, owner, repo, req)` builds a commit from the given set of changed files (blob creation, tree creation, commit creation against the branch's current head as parent) and advances the branch ref to the new commit SHA, returning the new commit SHA to the caller.
3. Neither method is reachable from the CLI flow unless Story 2's validation step has already reported success for the current working-tree state — the call site enforces this ordering, not the `Client` methods themselves.
4. All new HTTP calls reuse `postDo`/`get`'s existing retry-on-5xx/429 backoff and `redactSecrets` scrubbing — no new error path leaks a token or bypasses retry.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/17.0_auto_merged_fixes/`_

## Technical Considerations

- **Implementation Notes:** Extend `internal/ghaction/client.go` with `CreateBranch(ctx context.Context, owner, repo, branch, sha string) error` (`POST /repos/{owner}/{repo}/git/refs` with `ref: "refs/heads/"+branch, sha`) and `CreateCommit(ctx context.Context, owner, repo string, req CommitRequest) (string, error)`, where `CommitRequest` carries the target branch, commit message, base/parent SHA, and the set of changed files (path + content, or a deletion marker). `CreateCommit` orchestrates the Git Data API sequence: create a blob per changed file (`POST .../git/blobs`), create a tree referencing the base tree plus the blob changes (`POST .../git/trees`), create the commit object against the tree and parent (`POST .../git/commits`), then update the branch ref to the new commit SHA (`PATCH .../git/refs/heads/<branch>`). Both methods route through `postDo`/`get` for auth headers, retry, and redaction — no raw `http.Client` calls.
- **Integration Points:** `internal/ghaction/client.go` (extended, not replaced — same `Client` struct, same `APIError`, same `redactSecrets`); the orchestration entry point in the new `internal/autofix` package (from Story 1) is the caller that gates this story's methods behind Story 2's validation result; `cmd/atcr`'s `--auto-fix` command (Story 6) is the eventual CLI surface that wires token/repo/branch-name inputs through to these methods, mirroring the existing `--repo`/`--token`/`GITHUB_REPOSITORY`/`GITHUB_TOKEN` resolution in `cmd/atcr/github.go`.
- **Data Requirements:** No persistent schema — request/response shapes are transient Git Data API payloads (blob SHA, tree SHA, commit SHA, ref update). `CommitRequest` needs enough shape to carry multiple changed files (path, new content, and a deletion flag) since Story 1's apply step can touch several files in one patch and this story must express that as a single atomic commit, not one commit per file.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A commit or branch push executes before Story 2's validation has actually succeeded (an ordering bug at the call site), pushing a broken fix to GitHub where Story 3's local revert cannot reach it | High | Structure the `internal/autofix` orchestrator so `CreateBranch`/`CreateCommit` are only ever called from the single success branch of the validation step's result handling — no code path outside that branch has access to a token/repo config capable of invoking these methods. Cover with a test that asserts zero HTTP calls to `/git/refs` or `/git/blobs` when validation fails. |
| Branch name collision: a retried or concurrent auto-fix run targets a branch name that already exists, causing `CreateBranch` to fail with a 422 | Medium | Generate branch names deterministically but uniquely (e.g. a stable prefix plus a timestamp or the source finding's identifier); treat a 422 "Reference already exists" as a recoverable condition the caller can decide to retry-with-suffix or surface as a clear error, rather than a generic failure. |
| Multi-file commit construction (blob → tree → commit → ref) is a four-call sequence; a failure partway through (e.g. tree creation succeeds but ref update fails) can leave orphaned Git objects or a branch that exists but points at the wrong commit | Medium | Each intermediate call already benefits from `postDo`'s retry/backoff on transient failures; on a non-retriable failure mid-sequence, surface a clear `APIError` identifying which step failed so the caller (and a human reviewing the run) knows the branch may need manual cleanup — orphaned blobs/trees are inert and GitHub garbage-collects them, so the only stateful risk is a stale or missing branch ref. |
| Reusing the single shared `Client` for both read-only (check-run/comment) and now write-mutating (branch/commit) operations increases the blast radius of a token-scope misconfiguration | Low | No new client is introduced (per plan Technical Planning Notes), so this risk is inherent to the existing design, not new; document the required `contents:write`/`repo` scope in the `--auto-fix` CLI help text (Story 6) so misconfiguration fails fast with a clear permissions error rather than silently. |

---

**Created:** July 02, 2026 10:15:40PM
**Status:** Draft - Awaiting Acceptance Criteria

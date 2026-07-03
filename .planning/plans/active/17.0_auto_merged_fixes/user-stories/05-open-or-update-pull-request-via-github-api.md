# User Story 5: Open or Update Pull Request via GitHub API

**Plan:** [17.0: Auto-Merged Fixes Execution](../plan.md)

## User Story

**As an** ATCR maintainer running `--auto-fix`
**I want** ATCR to open a pull request for the branch/commit it just pushed, or update that PR if one already exists for the branch, once local validation has passed
**So that** an auto-fix always lands as something a human reviews and merges, never as a silent, unreviewed change to the target branch

## Story Context

- **Background:** Today's `cmd/atcr/github.go` only posts findings/comments onto an *existing* PR (`atcr github --inline-comments --pr <number>`) — there is no code path that creates a PR. This story adds that missing create/open half of AC5, layered directly on top of Story 4's branch and commit. It is the last step of the auto-fix flow: it surfaces the fix for human review rather than merging it, keeping a person in the approval chain. `internal/ghaction/client.go`'s `Client` (with its `postDo`/`get` request plumbing, retry/backoff, and secret redaction) is the reuse anchor — this story adds `CreatePullRequest` and `UpdatePullRequest` methods to that same `Client` rather than a new HTTP client.
- **Assumptions:** Story 4 has already created the branch and pushed the verified commit before this story runs; this story only ever opens/updates a PR for a branch that already exists remotely. A GitHub token with pull-request write scope and the target owner/repo are available via the same config surface Story 6's `--auto-fix` gate validates. Exactly one PR should exist per auto-fix branch — this story is responsible for checking whether one is already open for the branch before deciding to create vs. update.
- **Constraints:** No merge action of any kind — this story only creates or updates a PR object, it never calls a merge endpoint. Must not run before Story 4's commit/push has succeeded; a missing branch/commit is a hard precondition failure, not something this story can recover from. Must reuse `Client.postDo`/`get` (retry/backoff, typed `APIError`, secret redaction) rather than issuing raw HTTP calls, consistent with every other `internal/ghaction` method. PR title/body content must not leak secrets (tokens, credentials) even indirectly through fix diagnostics.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 4 (AC5, create branch and commit the verified fix) must have pushed the branch/commit this PR references. Feeds into User Story 6 (`--auto-fix` gate), which governs whether this flow runs at all. |

## Success Criteria (SMART Format)

- **Specific:** After Story 4 pushes a branch and commit, `internal/ghaction.Client` gains `CreatePullRequest` and `UpdatePullRequest` methods that open a new PR for the branch or update the existing one, and this behavior is wired into the `--auto-fix` orchestration as its final step.
- **Measurable:** 100% of successful `--auto-fix` runs (branch pushed, validation passed) result in exactly one open PR per branch — no duplicate PRs are created for a branch that already has one open, verified against `list pulls?head=owner:branch` (or equivalent) before create-vs-update is decided.
- **Achievable:** Extends the existing `Client` and its `postDo`/`get` plumbing with two new REST calls (`POST /repos/{owner}/{repo}/pulls`, `PATCH /repos/{owner}/{repo}/pulls/{number}`) plus a lookup call, following the exact pattern already established by `CreateCheckRunWithID`.
- **Relevant:** This is the human-in-the-loop closing step the plan's design principle depends on — auto-fix surfaces changes for review rather than merging them silently, so a broken or unwanted fix is always caught by a person before it reaches the target branch.
- **Time-bound:** PR create/update completes within the same per-run budget as the rest of the `--auto-fix` flow, using the `Client`'s existing retry/backoff and timeout behavior rather than a new waiting strategy.

## Acceptance Criteria Overview

1. `Client.CreatePullRequest` opens a new PR from the auto-fix branch against the configured base branch, with a title/body that identifies the fix, and returns the PR number.
2. Before creating, ATCR checks whether an open PR already exists for the branch; if one does, `Client.UpdatePullRequest` updates that PR (e.g. refreshed title/body) instead of creating a duplicate.
3. Both methods reuse `Client.postDo`/`get` for retry/backoff, typed `APIError` handling, and secret redaction, matching the conventions of `CreateCheckRunWithID` and `runGithub`.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/17.0_auto_merged_fixes/`_

## Technical Considerations

- **Implementation Notes:** Add `CreatePullRequest(ctx, owner, repo string, req PullRequestRequest) (int, error)` and `UpdatePullRequest(ctx, owner, repo string, prNumber int, req PullRequestRequest) error` to `internal/ghaction/client.go`, alongside a `PullRequestRequest` struct (head branch, base branch, title, body). Add a small existence check (GET `/repos/{owner}/{repo}/pulls?head={owner}:{branch}&state=open`) so the orchestrator can decide create-vs-update before calling either method — this can live as a private helper (`findOpenPullRequest`) or a small exported lookup, whichever keeps the `Client`'s existing method shape (`postDo`/`get` wrappers returning typed results). Route all HTTP through `postDo`/`get`, never `http.NewRequest` directly, so retry/backoff and secret redaction stay centralized in one place.
- **Integration Points:** Consumes the branch name and commit SHA produced by Story 4; this is the terminal step of the `internal/autofix` orchestration chain (parse → apply → validate → revert-or-continue → branch/commit → PR). Called only when Story 4 succeeds; a Story 4 failure means this story's methods are never invoked. Gated end-to-end by Story 6's `--auto-fix` flag and its refuse-without-backend check (a GitHub token/repo must already be validated before this step runs).
- **Data Requirements:** No new persistent schema. `PullRequestRequest` is an in-memory struct (head, base, title, body); the returned PR number is logged/reported as part of the `--auto-fix` run's output but not otherwise persisted by ATCR.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A second `--auto-fix` run on the same branch (e.g. re-running after amending the fix) creates a duplicate PR instead of updating the existing one | Medium | Always look up an existing open PR for the branch before deciding create-vs-update; treat "PR already exists" as the expected repeat-run case, not an error |
| PR title/body inadvertently includes secrets or sensitive diagnostic output pulled from validation logs | Medium | Reuse `Client`'s existing secret-redaction path (`redactSecrets`) on any dynamic content (validation stdout/stderr excerpts) before it is included in the PR body |
| GitHub API rate limiting or transient 5xx during PR create/update leaves a pushed branch with no PR, stranding the fix as an orphaned branch | Low | Reuse `postDo`'s existing retry/backoff so transient failures self-heal; on exhausted retries, surface a clear error naming the branch so the maintainer can open the PR manually |

---

**Created:** July 02, 2026 10:15:40PM
**Status:** Draft - Awaiting Acceptance Criteria

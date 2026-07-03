# GitHub API Orchestration (AC5)

## Purpose
This document details the GitHub API integration (AC5) for committing code changes, creating branches, and opening/updating Pull Requests.

## Design Principles
1.  **Reuse Existing Client**: We extend the existing [Client](file:///Users/samestrin/Documents/GitHub/atcr/internal/ghaction/client.go#L38-L47) in `internal/ghaction` rather than introducing a new dependency. This ensures that exponential back-off, error handling, and token authentication are shared.
2.  **Safe Ordering**: No remote mutations (creating refs, opening PRs) are performed until local validation (AC3) has succeeded. Remote actions are the final step in a successful flow.

## Implementation Details
The following methods will be added to `Client` in [internal/ghaction/client.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/ghaction/client.go):
-   `CreateBranch(ctx context.Context, owner, repo, branch, sha string) error`
-   `CreateCommit(ctx context.Context, owner, repo string, req CommitRequest) (string, error)`
-   `CreatePullRequest(ctx context.Context, owner, repo string, req PullRequestRequest) (int, error)`
-   `UpdatePullRequest(ctx context.Context, owner, repo string, prNumber int, req PullRequestRequest) error`

## Code References & Anchors
-   [internal/ghaction/client.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/ghaction/client.go): The target API client.
-   [cmd/atcr/github.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/github.go): CLI integration helper.

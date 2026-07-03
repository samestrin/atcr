# CLI Opt-In Gate (AC6)

## Purpose
This document details the `--auto-fix` command line flag (AC6), its configuration gate, and workflow execution.

## Design Principles
1.  **Fail-Closed Gate**: Auto-fix is completely opt-in. Without the `--auto-fix` flag, no write-back or remote mutations are attempted.
2.  **Backend Configuration Check**: Auto-fix requires a configured backend. The command must fail fast with a usage error (exit 2) if parameters like the GitHub Token, Repository, or Validation Command are missing.
3.  **Consistency**: Gating mirrors the `--exec` sandbox gating pattern implemented in [cmd/atcr/verify.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/verify.go#L45-L58).

## Implementation Details
-   Define `--auto-fix` flag inside [cmd/atcr/review.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go) and/or `cmd/atcr/autofix.go`.
-   Verify backend configuration presence:
    -   GitHub Repository (`GITHUB_REPOSITORY` env or `--repo` flag)
    -   GitHub Token (`GITHUB_TOKEN` env or `--token` flag)
    -   Local validation command (read from project configuration)
-   Refuse execution early if configuration is invalid or missing.

## Code References & Anchors
-   [cmd/atcr/main.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/main.go): Root CLI configuration.
-   [cmd/atcr/review.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/review.go): Gating logic and command flag registration.

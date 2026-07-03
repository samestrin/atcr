# Validation and Automatic Revert (AC3/AC4)

## Purpose
This document details the local validation step (AC3) and the automatic rollback mechanism (AC4) that restores files if validation fails.

## Design Principles
1.  **Fail-Safe Isolation**: Backups are created on a per-file basis prior to any modification. If validation fails, we restore each file exactly as it was.
2.  **Configurable Validation**: Validation is not hardcoded but accepts user configuration (e.g. custom test suites, `go build`, or linters) to be language-independent while maintaining safety.
3.  **Conservative Failure**: If the validation command exits with a non-zero code, it is treated as a failed fix, triggering immediate rollback.

## Implementation Details
-   **Backup Generation**: Before applying the patch, we call [BackupToDotBak](file:///Users/samestrin/Documents/GitHub/atcr/internal/atomicfs/atomic.go#L77-L146) for every modified file.
-   **Validation Trigger**: The validation step extends [internal/verify/syntaxguard.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/verify/syntaxguard.go) with a configurable mode to run custom build/linter commands.
-   **Revert Decision**:
    -   On validation success: remove temporary `.bak` files.
    -   On validation failure: restore the original file contents using `CopyPath` or `os.Rename` from the `.bak` files.

## Code References & Anchors
-   [internal/verify/syntaxguard.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/verify/syntaxguard.go): Primary validation entry point.
-   [internal/atomicfs/atomic.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/atomicfs/atomic.go): `BackupToDotBak` and file copy utility.

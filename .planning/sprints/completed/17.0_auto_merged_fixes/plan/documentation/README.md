# Feature Documentation Index

This directory contains the documentation for Plan 17.0 (Auto-Merged Fixes Execution).

## Documentation Files

### 1. [Patch Application (AC2)](patch-application.md)
*   **Status:** Draft
*   **Focus:** Core write-path and safe unified diff application using the `go-gitdiff` library.
*   **Anchors:** [atomicfs.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/atomicfs/atomic.go)

### 2. [Validation and Automatic Revert (AC3/AC4)](validation-and-revert.md)
*   **Status:** Draft
*   **Focus:** Extending syntax guard validation and implementing file-level rollback on failures.
*   **Anchors:** [syntaxguard.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/verify/syntaxguard.go), [atomic.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/atomicfs/atomic.go)

### 3. [GitHub API Orchestration (AC5)](github-orchestration.md)
*   **Status:** Draft
*   **Focus:** Extends the existing GitHub client to support branch, commit, and pull request creation.
*   **Anchors:** [client.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/ghaction/client.go)

### 4. [CLI Opt-In Gate (AC6)](cli-opt-in-gate.md)
*   **Status:** Draft
*   **Focus:** Opt-in flag gating, configuration checking, and fail-fast validation.
*   **Anchors:** [main.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/main.go), [flags.go](file:///Users/samestrin/Documents/GitHub/atcr/cmd/atcr/flags.go)

---

## Global Specifications Reference
*   [Coding Standards](file:///Users/samestrin/Documents/GitHub/atcr/.planning/specifications/coding-standards.md)
*   [Git Strategy & Workflow](file:///Users/samestrin/Documents/GitHub/atcr/.planning/specifications/git-strategy.md)
*   [Implementation Standards](file:///Users/samestrin/Documents/GitHub/atcr/.planning/specifications/implementation-standards.md)
*   [go-gitdiff package spec](file:///Users/samestrin/Documents/GitHub/atcr/.planning/specifications/packages/go-gitdiff.md)

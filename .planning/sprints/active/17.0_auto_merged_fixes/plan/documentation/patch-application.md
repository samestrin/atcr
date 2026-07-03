# Patch Application (AC2)

## Purpose
This document details the design and implementation for applying parsed unified diff patches safely to the working directory without corrupting files (AC2).

## Key Design Principles
1.  **Strict Atomicity**: No partially written files should ever be visible to readers or compilers. Every write is performed through the sibling-temp-rename approach provided by `WriteFileAtomic`.
2.  **No Hand-Rolled Parsers**: Hunk matching, fuzzy context tolerance, and offset drift are complex and prone to edge-case bugs. We use the third-party library `github.com/bluekeyes/go-gitdiff` to handle parsing and applying patches programmatically.

## Implementation Details
-   **Package Location**: `internal/autofix` (new package)
-   **Input**: `[]payload.FileEntry` representing the files and their patch contents.
-   **Process**:
    1.  Parse the patch string using `gitdiff.Parse(r io.Reader)`.
    2.  For each file, read the current source content from the filesystem.
    3.  Apply the parsed hunks using `gitdiff.Apply(w io.Writer, r io.Reader, f *gitdiff.File)`.
    4.  Write the resulting content back to the target path atomically via [WriteFileAtomic](file:///Users/samestrin/Documents/GitHub/atcr/internal/atomicfs/atomic.go#L24-L44).

## Code References & Anchors
-   [internal/atomicfs/atomic.go](file:///Users/samestrin/Documents/GitHub/atcr/internal/atomicfs/atomic.go): Provides `WriteFileAtomic` and `BackupToDotBak`.
-   [package-recommendations.md](file:///Users/samestrin/Documents/GitHub/atcr/.planning/plans/active/17.0_auto_merged_fixes/package-recommendations.md): Recommends `go-gitdiff` and explains its integration.

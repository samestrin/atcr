# User Story 1: Apply a Parsed Patch to the Working Tree Without Corruption

**Plan:** [17.0: Auto-Merged Fixes Execution](../plan.md)

## User Story

**As an** ATCR maintainer running `atcr` with the opt-in auto-fix flow
**I want** ATCR to write a parsed diff's changes to my working tree using fuzzy hunk matching and atomic per-file writes
**So that** a generated fix lands on disk exactly as intended, with no half-written file ever visible to my editor, my compiler, or a concurrent reader — even if a hunk's context has drifted or the process is interrupted mid-apply

## Story Context

- **Background:** AC1 (diff parsing into `[]payload.FileEntry`) already shipped via `internal/payload/ingest.go`'s `BuildEntriesFromDiff` — this story does not touch that parser. This story is the next link in the chain: take those parsed entries and actually apply them to files on disk. It is the foundational write-path for the whole auto-merge flow (PLAN_GOAL) — validation (AC3), revert (AC4), and the GitHub branch/PR flow (AC5) all assume patches already landed on disk correctly, so none of that later work can be trusted until this story is solid.
- **Assumptions:**
  - The target files being patched already exist in the working tree in a state consistent with the diff's `--- a/<path>` old-side content (or the entry represents a new-file creation, i.e. `--- /dev/null`).
  - The diff was produced against a clean, non-diverged target (per plan Out of Scope: complex merge conflict resolution is explicitly excluded).
  - `github.com/bluekeyes/go-gitdiff` (`gitdiff.Parse` / `gitdiff.Apply`) is the parsing/hunk-matching engine, not a hand-rolled patch applier — hand-rolling fuzzy context matching and offset drift is exactly the class of bug this library exists to avoid.
  - `internal/atomicfs.WriteFileAtomic` is the only path by which patched bytes touch the destination file — no direct `os.WriteFile`/`os.Create` on a target path.
- **Constraints:**
  - Strict atomicity: every write goes through the sibling-temp-then-rename pattern in `internal/atomicfs/atomic.go`; a reader must never observe a partially written file.
  - New package `internal/autofix` (file `apply.go`) is the home for this logic — a thin orchestrator over `go-gitdiff` + `atomicfs`, not a new parser or a new atomic-write primitive.
  - Input shape is fixed: `[]payload.FileEntry{Path, Size, Body}` from `BuildEntriesFromDiff` — this story consumes that shape as-is and must not redesign it.
  - A single patch commonly touches multiple files; the AC4 revert story that follows this one needs a per-file backup, not a directory-wide one, so this story's apply step must back up each file individually before writing it (via `atomicfs.BackupToDotBak`), even though revert logic itself belongs to a later story.
  - Out of scope: validation of the applied result (AC3), auto-revert on validation failure (AC4), and any GitHub API/branch/PR orchestration (AC5) — this story stops once the working tree reflects the patch.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None (consumes already-shipped `internal/payload.BuildEntriesFromDiff` and `internal/atomicfs` as reuse anchors; does not depend on any other story in this plan) |

## Success Criteria (SMART Format)

- **Specific:** Given a `[]payload.FileEntry` produced by `BuildEntriesFromDiff`, `internal/autofix` applies every entry's patch content to its target path in the working tree, producing the correct post-patch file content for modifications, new-file creations, and deletions.
- **Measurable:** 100% of applied files pass a byte-for-byte content check against the expected post-patch state in the test suite; zero test runs observe a truncated, empty, or partially written target file at any point during application (verified via a concurrent-reader/interrupted-write test against `atomicfs.WriteFileAtomic`'s rename guarantee).
- **Achievable:** The heavy lifting (fuzzy hunk matching, offset tolerance) is delegated to `go-gitdiff`, and the atomic-write mechanics already exist in `internal/atomicfs` — this story is integration and per-file orchestration, not new low-level primitives.
- **Relevant:** This is the write-path every later story in the plan (validation, revert, branch/PR) depends on; a correct, corruption-free apply here is what makes the rest of the auto-fix flow trustworthy enough to run unattended.
- **Time-bound:** Deliverable within this plan's sprint cycle, as the first story executed (Theme 1) since every subsequent story's local-write assumptions depend on it.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-parse-and-apply-hunks.md) | Parse and Apply Hunks for Modify, Create, and Delete Entries | Unit |
| [01-02](../acceptance-criteria/01-02-atomic-write-to-target-path.md) | Every Target Write Goes Through Atomic Sibling-Temp-Then-Rename | Unit |
| [01-03](../acceptance-criteria/01-03-per-file-backup-before-overwrite.md) | Per-File Backup via BackupToDotBak Before Any Overwrite | Unit |
| [01-04](../acceptance-criteria/01-04-per-file-error-isolation.md) | A Failed Hunk Reports a Clear Per-File Error Without Corrupting Prior Successes | Unit |

## Original Criteria Overview

1. `internal/autofix` parses each `payload.FileEntry.Body` via `gitdiff.Parse` and applies the resulting hunks via `gitdiff.Apply` against the current on-disk content of `FileEntry.Path`, correctly handling file modification, new-file creation (`/dev/null` old side), and file deletion.
2. Every file write to a target path goes through `atomicfs.WriteFileAtomic` (sibling-temp-then-rename) — no target file is ever visible in a partially written state to a concurrent reader.
3. Before a target file is overwritten, its pre-patch state is preserved individually via `atomicfs.BackupToDotBak` (one backup per touched file, not one per directory/patch), so a later revert story can restore file-by-file.
4. A hunk that fails to apply (context drifted beyond `go-gitdiff`'s fuzzy-match tolerance, target path missing when the diff expects it to exist, or vice versa) is reported as a clear per-file error without corrupting any file that succeeded before it in the same batch.


## Technical Considerations

- **Implementation Notes:** New package `internal/autofix`, file `apply.go`. Core flow per `payload.FileEntry`: (1) read current target content from disk (or treat as empty for a new-file entry), (2) `gitdiff.Parse` the entry's `Body` into a `*gitdiff.File`, (3) `gitdiff.Apply` against the current content to produce the patched bytes in memory, (4) `atomicfs.BackupToDotBak` the existing target (no-op if it doesn't yet exist — new-file case), (5) `atomicfs.WriteFileAtomic` the patched bytes to the target path. Process entries independently so one file's apply failure doesn't block or corrupt another file's already-completed write in the same run.
- **Integration Points:** `internal/payload.BuildEntriesFromDiff` (upstream producer of the `[]FileEntry` input — reference only, not modified), `internal/atomicfs.WriteFileAtomic` and `internal/atomicfs.BackupToDotBak` (both reused as-is), `github.com/bluekeyes/go-gitdiff` (`gitdiff.Parse`, `gitdiff.Apply` — new dependency, see `.planning/specifications/packages/go-gitdiff.md`). This story's output (files written + backups created) is what the AC4 revert story and the AC3 validation story build on next.
- **Data Requirements:** No persistent schema or database involved — purely filesystem state. The only "data" produced is the set of `.bak` files `atomicfs.BackupToDotBak` creates as a side effect, which a later story (AC4 revert) will consume; this story does not need to track or return that manifest beyond what `BackupToDotBak`'s return value already provides per call.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A hunk's context has drifted enough that `go-gitdiff`'s fuzzy matching mis-applies it (wrong location) rather than failing cleanly | High | Treat any non-nil error from `gitdiff.Apply` as a hard failure for that file (no partial-confidence apply); rely on `go-gitdiff`'s built-in match/offset tolerance rather than adding a custom leniency layer that could silently misplace a hunk. |
| Multi-file patch: one file's apply fails after other files in the same batch already succeeded, leaving the tree in a mixed patched/unpatched state | Medium | Per-file processing already isolates failures to that file; because every prior successful write went through its own `BackupToDotBak`, a mixed state is fully recoverable by the AC4 revert story — this story's job is only to ensure the mixed state is never corrupt, not to prevent it from occurring. |
| New-file creation and file-deletion diff entries are structurally different from a modification and can be mishandled by code written primarily against the modification case | Medium | Explicitly branch on `/dev/null` old/new-side markers (already surfaced by `payload.FileEntry.Path` resolution logic) and cover both cases directly in tests rather than assuming a single code path handles all three. |
| Backing up every touched file individually (rather than the tree as a whole) adds per-file I/O overhead on large patches | Low | `BackupToDotBak` is already the existing, tested primitive for this; per-file granularity is a stated constraint (needed for AC4's file-by-file revert), and patch sizes in the target use case (technical-debt fixes) are small enough that this overhead is not expected to be material. |

---

**Created:** July 02, 2026 10:15:40PM
**Status:** Draft - Awaiting Acceptance Criteria

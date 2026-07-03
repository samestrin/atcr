# User Story 3: Automatic Revert on Validation Failure

**Plan:** [17.0: Auto-Merged Fixes Execution](../plan.md)

## User Story

**As an** ATCR maintainer running `--auto-fix` against flagged technical debt
**I want** every patched file to be automatically restored to its pre-patch content the moment local validation fails
**So that** a bad auto-applied fix never leaves my working tree in a patched-but-broken state, and no GitHub branch, commit, or PR is ever created from an unvalidated change

## Story Context

- **Background:** User Story 1 (AC2) applies a parsed patch to the working tree; User Story 2 (AC3) runs a configurable validation command (`go build`, linter, formatter, etc.) against the patched tree. This story is the safety net between them: when validation reports failure, the patch must be undone file-by-file before `--auto-fix` does anything else, including any remote-mutating call. This is the mechanism that makes the plan's success criterion — "zero broken builds introduced by auto-merged fixes" — actually true rather than aspirational.
- **Assumptions:**
  - AC2's apply step has already created a per-file backup (via `atomicfs.BackupToDotBak`) for every file the patch touches, before any write to the live file happens.
  - Validation (AC3) reports a single pass/fail signal (plus captured output for logging) for the whole patched tree, not a per-file result — so revert-on-failure is "restore every file this patch touched," not selective per-file accept/reject.
  - A patch can touch multiple files in one apply; a partial apply (e.g. apply succeeds on 2 of 3 files before an unrelated failure) must still be revertible per-file rather than assuming all-or-nothing.
- **Constraints:**
  - Revert is strictly local-only and must complete in full before any `internal/ghaction` call (branch create, commit, PR) executes — sequencing order is a hard requirement, not an optimization, per the plan's Risk Mitigation section.
  - Must reuse `internal/atomicfs.BackupToDotBak` (backup-side, already used by AC2) and `internal/atomicfs.CopyPath` (restore-side) rather than introducing a second backup mechanism; the per-file revert shape mirrors `internal/fanout/reviewdir.go`'s `restorePriorBackup` pattern but adapted from a single directory-wide `.bak` to N per-file `.bak`s.
  - On the success path, `.bak` files are cleanup artifacts, not permanent state — they must be removed once validation passes so they do not accumulate across repeated `--auto-fix` runs or get mistaken for the working file.
  - A failure during the restore itself (e.g. a `.bak` file was deleted out-of-band) must be surfaced loudly, not swallowed — a silent restore failure would leave a broken file in the tree with no indication anything went wrong.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (Apply a parsed patch — AC2, produces the per-file `.bak` backups this story restores from); User Story 2 (Run a configurable local validation step — AC3, produces the pass/fail signal that triggers this story) |

## Success Criteria (SMART Format)

- **Specific:** Given a patch applied to N files where the post-apply validation command exits non-zero, every one of the N files is restored to its exact pre-patch byte content, and the `--auto-fix` run exits with a clear failure message before any GitHub API call is attempted.
- **Measurable:** 100% of files touched by a failed-validation patch are bit-for-bit identical to their pre-patch state after revert, verified by a checksum comparison in tests across single-file and multi-file patch scenarios, including a simulated partial-apply failure.
- **Achievable:** Reuses `atomicfs.BackupToDotBak` (backup) and `atomicfs.CopyPath` (restore) verbatim — no new low-level file I/O primitives are needed, only the orchestration layer (`internal/autofix/revert.go`) that tracks which `.bak` belongs to which touched file and drives the restore loop.
- **Relevant:** Directly delivers AC4 and is the mechanism behind the plan's stated success criterion "a validation failure never leaves the working tree in a patched-but-broken state."
- **Time-bound:** Implementable within the sprint allocated to this plan's AC2–AC4 slice; unblocks AC5 (User Stories 4–5) which must not begin until this revert path is proven correct, since AC5 is explicitly sequenced after local validation passes.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [03-01](../acceptance-criteria/03-01-backup-map-tracking.md) | Per-File Backup Map Precondition and Tracking | Unit |
| [03-02](../acceptance-criteria/03-02-restore-on-validation-failure.md) | Restore All Touched Files on Validation Failure | Unit/Integration |
| [03-03](../acceptance-criteria/03-03-cleanup-on-validation-success.md) | Cleanup Backup Files on Validation Success | Unit |
| [03-04](../acceptance-criteria/03-04-hard-error-on-restore-failure.md) | Hard Error Surfacing on Restore Failure | Unit |

## Original Criteria Overview

1. Before any file in a patch is modified, a per-file backup exists (produced by AC2/User Story 1) that this story's revert path can restore from.
2. When the configured validation command (AC3/User Story 2) exits non-zero, every touched file is restored from its `.bak` via `atomicfs.CopyPath`/`os.Rename`, and this restore completes fully before control returns to the `--auto-fix` orchestrator — no GitHub-mutating call may occur if any restore is pending or failed.
3. When validation succeeds, all `.bak` files created for this patch are removed so no stale backup state persists between `--auto-fix` runs; when a restore itself fails, the failure is surfaced as a hard error naming the still-diverged file(s) rather than silently continuing.


## Technical Considerations

- **Implementation Notes:** New file `internal/autofix/revert.go`, modeled on `internal/fanout/reviewdir.go`'s `restorePriorBackup` (stage-aside, restore-on-failure, log a Warn if the restore itself cannot complete) but generalized from one directory-wide `.bak` to a per-file map of `{originalPath -> backupPath}` collected during the AC2 apply step. The revert function takes that map and, on validation failure, iterates it calling `atomicfs.CopyPath(backupPath, originalPath)` (or `os.Rename` where a same-filesystem move suffices) for every entry, collecting and returning any restore errors rather than stopping at the first one — a partial revert must still attempt every remaining file so failure is localized to the smallest possible set.
- **Integration Points:** Consumes the backup map produced during User Story 1's apply step (AC2) and the pass/fail signal produced by User Story 2's validation step (AC3). Sits strictly before User Story 4/5's `internal/ghaction` calls (AC5) in the `--auto-fix` orchestrator's control flow in `cmd/atcr` — the orchestrator must not proceed to branch/commit/PR creation until this story's revert-or-cleanup step has returned without error.
- **Data Requirements:** No new persistent schema; the only "data" is the transient per-file `{originalPath -> .bak path}` mapping held in memory for the duration of a single `--auto-fix` invocation, plus the `.bak` files themselves on disk during the apply→validate window (removed on success, consumed on failure).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A GitHub-mutating call (branch/commit/PR) fires before local revert completes, due to a sequencing bug in the orchestrator. | High | Revert is a hard synchronous gate in `--auto-fix`'s control flow: the orchestrator function signature makes it structurally impossible to reach the AC5 step without first receiving a "validated + cleaned up" or "reverted + failed" terminal result from this story's function — no fire-and-forget or async revert. |
| Partial apply (patch touches 3 files, only 2 successfully backed up before an apply-time error) leaves the third file's true prior state unknown, so revert cannot guarantee full restoration. | Medium | Per Story 1/AC2's ordering, a backup for a file is created before that file is written; a file whose write never happened needs no restore. The revert map only ever contains entries for files that were actually backed up, so revert coverage exactly matches write coverage — never wider, never narrower. |
| A `.bak` file is missing or corrupted at restore time (e.g. disk pressure evicted it, or an out-of-band process touched it), making a clean restore impossible. | Medium | Restore failure for any single file is reported as a hard error naming the specific file and its expected `.bak` path, and the overall `--auto-fix` run is marked failed rather than reporting false success — matches `restorePriorBackup`'s existing precedent of logging (not swallowing) an unrecoverable restore. |
| Stale `.bak` files accumulate across repeated `--auto-fix` runs if cleanup-on-success is skipped or crashes mid-run. | Low | Cleanup runs immediately after a validation-success signal, in the same synchronous path as the failure-triggered restore; a leftover `.bak` from a crashed prior run is a known, inspectable artifact (same acceptance as `atomicfs`'s existing "garbage-collecting older .bak state is the caller's job" convention) rather than a silent correctness risk. |

---

**Created:** July 02, 2026 10:15:40PM
**Status:** Refined - Ready for Execution

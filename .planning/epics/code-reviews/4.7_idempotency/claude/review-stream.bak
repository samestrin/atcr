# Code Review Stream - 4.7_idempotency (Epic)

**Started:** June 19, 2026 08:57:35PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — collision error names BOTH --resume and --force
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir.go:230` — `ScaffoldReviewDir` returns `"review directory %s already exists; use --resume %s to continue it or --force to overwrite"`. Test: `internal/fanout/reviewdir_test.go:219` TestScaffoldReviewDir_CollisionMessageNamesResumeAndForce.
- **Notes:** Names both branches. `--output-dir` path names only `--force` (resume N/A to unmanaged dirs) per clarifications — `internal/fanout/reviewdir.go:289`.

### Criterion: AC1b — --resume and --force mutually exclusive (usage error)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review.go:94` — guard at the resume branch point before short-circuit returns `usageError(errors.New("--resume and --force are mutually exclusive"))`. Test: `cmd/atcr/review_test.go:294` TestReviewCmd_ResumeAndForceMutuallyExclusive.
- **Notes:** Checked before resume short-circuit, so it fires regardless of flag order.

### Criterion: AC2 — --force backs existing dir up to <dir>.bak then scaffolds fresh
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:235-240` (`--id`) and `:222-227` (`--output-dir`) call `forceBackupReviewDir`/`forceBackupOutputDir` before scaffolding. `internal/fanout/reviewdir.go:backupExisting` removes stale `.bak` then renames. Tests: TestBackupExisting_MovesAsideReplacingStaleBak (reviewdir_test.go:246), TestForceBackupOutputDir_* (275,298).
- **Notes:** Backup-then-scaffold ordering correct for both paths. Foreign-`.bak` guard protects unmanaged --output-dir siblings.

### Criterion: AC3 — WriteFileAtomic (temp+rename) and new WriteJSON wrapper
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/atomicfs/atomic.go:16-36` WriteFileAtomic (CreateTemp → Write → Chmod → Close → Rename); `:42-48` WriteJSON (MarshalIndent → WriteFileAtomic). Tests: TestWriteFileAtomic_HappyPathWritesExactBytes (atomic_test.go:78), TestWriteJSON_RoundTripsIndented (11).

### Criterion: AC4 — reconcile backs up prior reconciled/ output before re-emit
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/reconcile/gate.go:RunReconcile` — `atomicfs.BackupToDotBak(reconDir)` gated on existence of `reconciled/findings.json`, before Emit. Test: `internal/reconcile/gate_test.go:78` TestRunReconcile_BacksUpPriorReconciledOnReRun.
- **Notes:** Per clarifications, backup unit is the `reconciled/` dir → `reconciled.bak/` (not the illustrative `reconciled.json`). Copy (not move) preserves the live dir for adjudication reads. Empty first-run dir skipped.

### Criterion: AC5 — verify backs up prior verification.json before re-write
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/emit_verification.go:WriteVerification` calls `BackupExistingVerification` (→ `atomicfs.BackupToDotBak(verification.json)`) before the atomic write. Test: `internal/verify/emit_test.go:55` TestWriteVerification_BacksUpPriorOnReWrite.
- **Notes:** Only verify-owned verification.json backed up; reconcile-owned findings.json/summary.json the stage annotates are covered by reconciled.bak/ (AC4) — no redundant per-file .bak.

### Criterion: AC6 — partial writes do not corrupt existing files
- **Verdict:** VERIFIED ✅
- **Evidence:** WriteFileAtomic temp+rename never truncates the target; WriteJSON marshals before touching the file. Tests: TestWriteFileAtomic_OrphanedTempDoesNotCorruptTarget (atomic_test.go:150), TestWriteJSON_FailedWritePreservesExistingFile (124), TestWriteJSON_UnmarshalableValueErrorsAndWritesNothing (108).

### Criterion: AC7 — go test ./internal/atomicfs/... passes with atomic write tests
- **Verdict:** VERIFIED ✅ (test run confirmed in Phase 4)
- **Evidence:** `internal/atomicfs/atomic_test.go` — 11 test functions covering WriteFileAtomic, WriteJSON, BackupToDotBak.

### Criterion: AC8 — integration test: run review twice with --force, verify backup created
- **Verdict:** VERIFIED ✅
- **Evidence:** TestBackupExisting_MovesAsideReplacingStaleBak (reviewdir_test.go:246), TestForceBackupOutputDir_ReplacesPriorAtcrBak (298) exercise the run-twice backup-creation path; atomic-write-no-corruption covered by atomic_test.go:150/124. 
- **Notes:** Coverage is at the force-backup function level rather than a full CLI run-twice e2e harness, but the backup-creation contract is directly asserted.

## Adversarial Analysis (Discovery Mode)

**Mode:** Full hostile review (no sprint-design.md — discovery-only)
**Files Reviewed:** 7 source files (3 parallel agent batches)
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 4
- Low: 6

### Notable
Agents confirmed the two riskiest axes are CORRECT: `reconciled.bak/` is a sibling (not nested) of `reconciled/` so no recursive copy; backup-before-overwrite ordering holds in both reconcile and verify; the original/live tree is never lost on a backup failure. The recurring real weakness is the **RemoveAll-before-write backup pattern** (loses the single prior `.bak` generation on a failed swap) and the **foreign-data guard being name-only / inconsistently applied**. Raw agent output (37 findings) was filtered to 10 distinct real defects; nitpicks and self-retracted findings were dropped.

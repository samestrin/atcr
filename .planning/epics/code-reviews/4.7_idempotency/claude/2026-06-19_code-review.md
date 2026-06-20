# Code Review Report: 4.7_idempotency

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 9 / 9
- **Approval Status:** Approved
- **Review Date:** June 19, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **4.7_idempotency.md** – AC1 collision error names both --resume and --force
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/reviewdir.go:230`
- **4.7_idempotency.md** – AC1b --resume/--force mutually exclusive
  - Before: `[ ]` → After: `[x]`
  - Evidence: `cmd/atcr/review.go:94`
- **4.7_idempotency.md** – AC2 --force backs up dir to <dir>.bak
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/review.go:235-240`, `internal/fanout/reviewdir.go:283-306`
- **4.7_idempotency.md** – AC3 WriteFileAtomic + WriteJSON
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/atomicfs/atomic.go:16-48`
- **4.7_idempotency.md** – AC4 reconcile backs up reconciled/
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/reconcile/gate.go:187-191`
- **4.7_idempotency.md** – AC5 verify backs up verification.json
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/verify/emit_verification.go:144-167`
- **4.7_idempotency.md** – AC6 partial writes do not corrupt
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/atomicfs/atomic.go:16-48`
- **4.7_idempotency.md** – AC7 atomicfs tests pass
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/atomicfs/atomic_test.go` (11 tests, pass)
- **4.7_idempotency.md** – AC8 run-twice-with-force backup test
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/reviewdir_test.go:246,298`

## 3. Evidence Map
- **AC1/AC1b — collision + mutual-exclusion** `internal/fanout/reviewdir.go:230`, `cmd/atcr/review.go:94`. The `ScaffoldReviewDir` collision error names both `--resume <id>` and `--force`; the `--resume`/`--force` guard fires at the resume branch point before short-circuit, so flag order is irrelevant.
- **AC2 — force backup** `internal/fanout/review.go:222-240`. Backup-then-scaffold ordering for both `--id` and `--output-dir`; `backupExisting` removes stale `.bak` then renames; foreign-`.bak` guard protects unmanaged output-dir siblings.
- **AC3/AC6 — atomic writes** `internal/atomicfs/atomic.go:16-48`. WriteFileAtomic = CreateTemp→Write→Chmod→Close→Rename; WriteJSON marshals before touching the file.
- **AC4 — reconcile backup** `internal/reconcile/gate.go:187-191`. `BackupToDotBak(reconDir)` gated on `findings.json` existence, copy-not-move, before Emit.
- **AC5 — verify backup** `internal/verify/emit_verification.go:155-167` + `internal/verify/pipeline.go:353`. `BackupExistingVerification` runs before the atomic re-write.
- **AC7/AC8 — tests** atomicfs 11 tests; reviewdir backup tests; reconcile/verify re-run backup tests. Full suite green.

## 4. Remaining Unchecked Items
No remaining unchecked items - all 9 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All acceptance criteria implemented with test coverage; full suite passes at 89.7% coverage; lint/vet/format clean. Adversarial review surfaced no critical/high defects — only quality-hardening follow-ups (data-durability edge cases on the backup swap, defense-in-depth path validation, a dead-code test seam).

## 6. Coverage Analysis
- **Coverage:** 89.7%
- **Baseline:** 80%
- **Delta:** ↑9.7%
- **Status:** PASSING
- **Note:** `internal/atomicfs` package alone is 75.5%; uncovered lines are syscall-failure error branches (CreateTemp/Write/Chmod/RemoveAll faults) that require fault injection to exercise.

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 7 source files (3 parallel hostile-review batches)
- **Issues Found:** 10 (Critical: 0, High: 0, Medium: 4, Low: 6)

### Issues by Severity
**MEDIUM**
1. `internal/fanout/reviewdir.go:285` / `internal/atomicfs/atomic.go:66` — RemoveAll-before-rename/copy loses the single prior `.bak` generation on a failed swap (incl. EXDEV on cross-mount `--output-dir`). Live tree safe; prior backup not.
2. `internal/fanout/review.go:217` — `validation.FilePath` (system-dir reject) enforced only at the CLI layer, not in exported `PrepareReview`; a direct/MCP caller with Force+OutputDir bypasses it before the destructive backup (not exploitable today).
3. `internal/verify/emit_verification.go:144` — `WriteVerification` wrapper has no non-test callers; production verify writes via `pipeline.go` + a direct `BackupExistingVerification`. Tests on the wrapper give false coverage confidence.
4. `internal/verify/pipeline.go:353` — standalone `atcr verify --fresh` overwrites findings.json/summary.json whose backup the AC5 docstring claims is "covered by reconciled.bak (AC4)", but reconciled.bak is written only by RunReconcile. Verify/document.

**LOW**
5. `internal/fanout/reviewdir.go:356` — `looksLikeReviewTree` provenance marker is name-only (payload/sources/reconciled); add a manifest.json check to harden the foreign-data guard.
6. `internal/atomicfs/atomic.go:35` — WriteFileAtomic does not fsync temp/parent dir; atomic rename prevents partial reads but not truncation after power-loss.
7. `internal/fanout/review.go:241` — `--force` with a derived id is a silent no-op; emit a notice.
8. `internal/fanout/reviewdir.go:304` — backup path computed but discarded; surface "backed up to <dir>.bak" breadcrumb.
9. `internal/reconcile/gate.go:188` — backup failure hard-aborts an otherwise-valid reconcile re-run; consider warn-and-continue + cleanup of partial reconciled.bak.
10. `internal/reconcile/gate.go:187` — backup taken before adjudication validation; a validation-failed re-run pays a full-tree copy then errors, leaving a misleading `.bak`. Move backup just before Emit.

## 9. Follow-ups
None blocking. The 10 adversarial findings are quality-hardening technical debt; route via `/reconcile-code-review` into the TD README. No acceptance criterion is at risk.

---
*Generated by /execute-code-review on 2026-06-19 20:57:35*

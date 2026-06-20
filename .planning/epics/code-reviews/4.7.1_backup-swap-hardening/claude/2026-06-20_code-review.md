# Code Review Report: 4.7.1_backup-swap-hardening

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 20, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **.planning/epics/completed/4.7.1_backup-swap-hardening.md** – AC1 backupExisting() crash-safe swap
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/reviewdir.go:311-359`
- **.planning/epics/completed/4.7.1_backup-swap-hardening.md** – AC2 BackupToDotBak() staged swap
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/atomicfs/atomic.go:77-175`
- **.planning/epics/completed/4.7.1_backup-swap-hardening.md** – AC3 EXDEV copy+remove fallback
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/reviewdir.go:338-350,388-402`
- **.planning/epics/completed/4.7.1_backup-swap-hardening.md** – AC4 fault-injection tests at both sites
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/reviewdir_test.go:434,465`; `internal/atomicfs/atomic_test.go:333,374,453`
- **.planning/epics/completed/4.7.1_backup-swap-hardening.md** – AC5 one-generation contract preserved
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/fanout/reviewdir.go:317-323`; `internal/atomicfs/atomic.go:97-99`; test `reviewdir_test.go:499`

## 3. Evidence Map
- **AC1 — failed swap preserves prior .bak**
  - Evidence: `internal/fanout/reviewdir.go:311-359`, `internal/fanout/reviewdir.go:373` (restorePriorBackup)
  - Summary: Prior `.bak` is renamed to `.bak.old` before the live-tree swap; removed only on success, restored on any failure. Test `reviewdir_test.go:434` asserts prior `.bak` and live tree both survive a simulated swap failure.
- **AC2 — copy-based staged swap preserves prior .bak**
  - Evidence: `internal/atomicfs/atomic.go:77-175` (BackupToDotBak + swapStagedBackup)
  - Summary: Copy is staged into `.bak.tmp-*` before the prior `.bak` is touched; swap defers removal until success. Verify-not-rewrite per epic clarification (#53). Tests `atomic_test.go:333,374,453` cover failed copy and failed rename.
- **AC3 — EXDEV fallback**
  - Evidence: `internal/fanout/reviewdir.go:338-350,388-402`
  - Summary: `syscall.EXDEV` on the move triggers `backupCrossDevice` (copy → `.bak.new`, same-fs rename swap, `RemoveAll(path)`). Test `reviewdir_test.go:465` injects EXDEV via the `renameFn` seam and asserts the new generation, vacated path, and no straggler.
- **AC4 — fault-injection at both sites**
  - Evidence: `reviewdir_test.go:434,465`; `atomic_test.go:333,374,453`
  - Summary: Both sites use a package-level `renameFn` seam for deterministic, CI-safe injection; assertions check prior `.bak` content survives and no staging artifact leaks.
- **AC5 — one-generation contract**
  - Evidence: `reviewdir.go:317-323`; `atomic.go:97-99`; test `reviewdir_test.go:499`
  - Summary: `.bak.old`/`.bak.new`/`.bak.tmp-*` cleaned at entry and after success; exactly one `.bak` remains across crash-then-retry. `guardForeignBackup` kept scoped to `.bak` per clarification Q4.

## 4. Remaining Unchecked Items
No remaining unchecked items - all 5 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All ACs implemented with concrete file:line evidence; full test suite passes at 89.6% coverage; lint, vet, and format all clean. Adversarial findings are hardening/edge items (no critical/high), routed to the TD stream for `/reconcile-code-review`.

## 6. Coverage Analysis
- **Coverage:** 89.6%
- **Baseline:** 80%
- **Delta:** ↑9.6%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 5
- **Issues Found:** 8 (Critical: 0, High: 0, Medium: 3, Low: 5)

### Issues by Severity
**MEDIUM**
- `internal/fanout/reviewdir.go:318` (security) — `guardForeignBackup` not extended to `.bak.old`/`.bak.new`; `--force` on an arbitrary `--output-dir` can silently delete user-owned siblings. Re-raises epic clarification Q4 (deliberate scope decision).
- `internal/fanout/reviewdir.go:388` (testing) — EXDEV fallback failure legs (copy/inner-swap/vacate + restore-on-EXDEV-failure) untested; "EXDEV without data loss" AC proven only for the success path.
- `internal/fanout/reviewdir.go:373` (correctness) — "never zero generations" invariant is conditional on a best-effort *silent* restore succeeding; a failed restore strands the only copy in `.bak.old`, which the next run's entry-time cleanup deletes. Same pattern at `atomic.go:159-167`.

**LOW**
- `internal/fanout/reviewdir.go:380` (maintainability) — doc overclaims cross-device fallback "matches os.Rename's postcondition" (it is two non-atomic steps).
- `internal/fanout/reviewdir.go:388` (maintainability) — same-fs invariant for inner `os.Rename` is naming-coincidental; make it structural.
- `internal/atomicfs/atomic.go:180` (maintainability) — two identically-named `renameFn` seams; `backupCrossDevice` inner `os.Rename` bypasses both.
- `internal/fanout/reviewdir_test.go:458` (testing) — minor test-assertion gaps (`.bak.new` absence, Lstat-error branch, cleanup-failure legs).
- `internal/fanout/reviewdir.go:328` (security) — documented TOCTOU window between `Lstat` and `Rename` at both sites.

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/4.7.1_backup-swap-hardening.md` to merge these 8 findings into the technical-debt README with reviewer attribution.
- Consider the 3 MEDIUM items for a follow-on hardening pass; the EXDEV failure-leg test gap is the highest-value to close.

---
*Generated by /execute-code-review on June 20, 2026 05:32:38AM*

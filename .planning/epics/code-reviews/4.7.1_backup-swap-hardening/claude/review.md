# Code Review Stream - 4.7.1_backup-swap-hardening (Epic)

**Started:** June 20, 2026 05:32:38AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — backupExisting() leaves the prior .bak intact when the swap fails
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir.go:311-359` (stage prior `.bak`→`.bak.old`, swap, `RemoveAll` on success / `restorePriorBackup` on failure); test `internal/fanout/reviewdir_test.go:434`
- **Notes:** Prior generation renamed aside before the live-tree swap; `restorePriorBackup` (line 373) moves `.bak.old` back on any failure. Test asserts prior `.bak` and live tree both survive a simulated swap failure.

### Criterion: AC2 — BackupToDotBak() stages into a temp sibling and swaps atomically, prior .bak intact on partial copy
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/atomicfs/atomic.go:77-175` (stage into `.bak.tmp-*`, `swapStagedBackup` defers prior removal); tests `internal/atomicfs/atomic_test.go:333,374,453`
- **Notes:** Per epic clarification this was hardened by #53 and is verify-not-rewrite. Copy is staged before the prior `.bak` is touched; `.bak.new` plan naming was illustrative (impl uses `.bak.tmp-*`). Failed-copy and rename-step fault tests confirm the prior `.bak` survives.

### Criterion: AC3 — EXDEV on the move-based path detected, falls back to copy+remove without losing prior .bak
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir.go:338-350` (EXDEV branch) + `:388-402` (`backupCrossDevice`: copy→`.bak.new`, same-fs rename swap, `RemoveAll(path)`); test `internal/fanout/reviewdir_test.go:465`
- **Notes:** `renameFn` package-var seam (line 366) lets the test inject `syscall.EXDEV` deterministically. Fallback replicates move's vacate-path postcondition; prior `.bak` staged aside is restored on fallback error.

### Criterion: AC4 — Fault-injection tests cover a failed swap at both sites and assert prior .bak survives
- **Verdict:** VERIFIED ✅
- **Evidence:** move site `reviewdir_test.go:434` (failed swap), `:465` (EXDEV); copy site `atomic_test.go:453` (rename-step, file+dir), `:333`/`:374` (failed copy)
- **Notes:** Both sites use a package-level `renameFn` seam for deterministic, CI-safe injection. Every test asserts the prior `.bak` content survives and no staging artifact leaks.

### Criterion: AC5 — one-generation --force contract preserved (no backup accumulation)
- **Verdict:** VERIFIED ✅
- **Evidence:** entry-time straggler cleanup `internal/fanout/reviewdir.go:317-323` and `internal/atomicfs/atomic.go:97-99`; test `reviewdir_test.go:499`
- **Notes:** `.bak.old`/`.bak.new`/`.bak.tmp-*` are RemoveAll'd at entry and after success; exactly one `.bak` generation remains across crash-then-retry. `guardForeignBackup` stays scoped to `.bak` only, as the clarification specified.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md for an epic)
**Files Reviewed:** 5 (atomic.go, atomic_test.go, reviewdir.go, reviewdir_test.go, boundaries_test.go)
**Issues Found:** 8 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 8

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 3
- Low: 5

### Summary
All 5 acceptance criteria are fully implemented and covered by deterministic fault-injection tests; the suite passes (89.6% coverage). The adversarial findings are hardening/edge items, not blockers:
- The 3 MEDIUM items cluster on the EXDEV cross-device fallback: its failure legs (copy/inner-swap/vacate + restore-on-EXDEV-failure) are untested, and the "never zero generations" invariant is conditional on a best-effort *silent* restore succeeding (a failed restore strands the only copy in `.bak.old`, which the next run's entry-time cleanup deletes). One MEDIUM re-raises epic clarification Q4 (guard scope) re: `--force` deleting user-owned `.bak.old`/`.bak.new` on an arbitrary `--output-dir`.
- The 5 LOW items are doc-overclaim on cross-device atomicity, a naming-coincidental same-fs invariant, two identically-named `renameFn` seams, minor test-assertion gaps, and a documented TOCTOU window.

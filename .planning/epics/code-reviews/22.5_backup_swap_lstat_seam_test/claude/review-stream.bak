# Code Review Stream - 22.5_backup_swap_lstat_seam_test (Epic)

**Started:** July 13, 2026 07:18:21PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: backupExisting's non-ErrNotExist Lstat(backup) branch is covered by a test using the new seam
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir_test.go:524-556` (TestBackupExisting_LstatProbeFailureSurfaces) drives `lstatFn(backup)` to a non-ErrNotExist error via `withLstatStub` and asserts the wrapped `"checking prior backup"` error at `internal/fanout/reviewdir.go:346-347`.
- **Notes:** Test leaves the .bak.old/.bak.new RemoveAll legs on real os.Lstat so only the probe branch (reviewdir.go:341) is exercised; also asserts `ErrorIs(err, probeErr)`, live tree survival, and no backup created.

### Criterion: No behavior change to the production path — the seam defaults to os.Lstat when not overridden
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/reviewdir.go:401` `var lstatFn = os.Lstat`; only the line-341 probe was rerouted through `lstatFn`. The belt-and-suspenders `os.Lstat` at `reviewdir.go:362` was correctly left as `os.Lstat` (out of scope, no ErrNotExist branch).
- **Notes:** Seam mirrors the existing `renameFn`/`removePathFn` package-var pattern. Production default unchanged.

### Criterion: go test ./internal/fanout/... passes with the new test green
- **Verdict:** VERIFIED ✅ (confirmed in Phase 4 test run)
- **Evidence:** See Phase 4 — `internal/fanout` package tests including TestBackupExisting_LstatProbeFailureSurfaces.
- **Notes:** Verified by test execution in Phase 4.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md for epic)
**Files Reviewed:** 2 (internal/fanout/reviewdir.go, internal/fanout/reviewdir_test.go)
**Issues Found:** 2 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic mode)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 2

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 2

Both findings are LOW maintainability nits (comment accuracy / hardcoded line-number references in test doc comments). The hostile reviewer confirmed: production change is minimal and correctly scoped (only reviewdir.go:341 rerouted; :362/:252/:517 untouched), assertions are load-bearing and non-vacuous, no reward-hacking, no weakened assertions.

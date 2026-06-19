# Code Review Stream - 4.3_input_validation (Epic)

**Started:** June 19, 2026 07:34:30AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: AC1 — validation.GitRef("main") returns nil
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/validation/validation.go:29-41`, test `internal/validation/validation_test.go:18`
- **Notes:** Plain branch name passes all guards; test asserts NoError.

### Criterion: AC2 — validation.GitRef("invalid..ref") returns "contains invalid characters"
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/validation/validation.go:34-35`, test `internal/validation/validation_test.go:22-24`
- **Notes:** Test asserts exact message `invalid git ref "invalid..ref": contains invalid characters`.

### Criterion: AC3 — validation.FilePath("/etc/passwd") returns "must not reference system directories"
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/validation/validation.go:62-65`, test `internal/validation/validation_test.go:45-46`
- **Notes:** Directory-boundary match (exact or `<dir>/` prefix) avoids false positives on /etcd, /etc-backup; tested at line 55-57.

### Criterion: AC4 — validation.ReviewID("../../../etc/passwd") returns allowlist error
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/validation/validation.go:78-79`, test `internal/validation/validation_test.go:67-68`
- **Notes:** Test asserts exact message.

### Criterion: AC5 — validation.Severity("INVALID") returns "must be one of: LOW, MEDIUM, HIGH, CRITICAL"
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/validation/validation.go:88-92`, test `internal/validation/validation_test.go:80-81`
- **Notes:** Case-insensitive accept path tested at line 75-77.

### Criterion: AC6 — Validation errors returned as usageError (exit code 2)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review.go:74-75`, `cmd/atcr/report.go:47-48`; tests `review_test.go:332` (SystemDirRejected, exit 2) and `report_test.go:131` (SystemDirOutputIsUsageError, exit 2)
- **Notes:** Both FilePath wirings wrap the error in usageError; tests assert exit code 2.

### Criterion: AC7 — go test ./internal/validation/... passes with 100% coverage
- **Verdict:** VERIFIED ✅ (confirmed in Phase 4)
- **Evidence:** `internal/validation/validation_test.go` covers every branch of every validator.
- **Notes:** Coverage measured in Phase 4 test run.

### Criterion: AC8 — atcr review --base "invalid..ref" fails fast with clear error
- **Verdict:** VERIFIED ✅
- **Evidence:** Pre-existing `gitrange.Resolve` rejects `invalid..ref` via git before any agent/API call, returning usageError (exit 2).
- **Notes:** Per epic clarification Q1→A, GitRef is intentionally NOT wired onto --base/--head (those take revisions like HEAD^). Accepted trade-off: the message stays git-flavored. Fail-fast behavior satisfied.

## Adversarial Analysis (Discovery Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic)
**Files Reviewed:** 4 (validation.go, review.go, report.go, validation_test.go)
**Issues Found:** 5 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 5

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 3

Out-of-scope items (GitRef-on-base/head, ReviewID-on-id, Severity/Enum replacement, Windows volume detection) were explicitly excluded per epic clarifications.

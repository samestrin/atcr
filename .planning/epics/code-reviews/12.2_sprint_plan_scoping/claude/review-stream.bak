# Code Review Stream - 12.2_sprint_plan_scoping (Epic)

**Started:** June 27, 2026 01:41:32PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — `atcr review --sprint-plan <path>` is a valid CLI flag
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review.go:54`, `cmd/atcr/review.go:90-96` (sprintPlanPath), `cmd/atcr/review.go:234`
- **Notes:** Flag registered on the review cmd; value trimmed and mapped to `ReviewRequest.SprintPlanPath`.

### Criterion: AC2 — Missing/empty plan proceeds without warnings
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/sprintplan.go:31-46`, `internal/payload/sprintplan.go:63-67`, `internal/fanout/review.go` resolveScopeConstraint
- **Notes:** Empty path and `os.IsNotExist` both return `("",nil)`; empty/whitespace content returns `("",false)`; resolveScopeConstraint returns `("","")` so no stderr warning fires.

### Criterion: AC3 — Unreadable plan warns on stderr, proceeds
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/sprintplan.go:35-44`, `internal/fanout/review.go` resolveScopeConstraint + `fmt.Fprintln(os.Stderr, "warn: "+scopeWarn)` in PrepareReview/PrepareReviewFromDiff
- **Notes:** Non-NotExist read errors surface a warning; constraint returned empty so the review proceeds diff-wide.

### Criterion: AC4 — Valid plan injects SCOPE CONSTRAINT before diff payload
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/sprintplan.go:63-81`, `internal/fanout/review.go` buildAgent `Payload: scopeConstraint + mp.Text`
- **Notes:** Block prepended to {{.Payload}} which every persona renders, placing the constraint immediately before the diff (NFR satisfied, template-agnostic).

### Criterion: AC5 — Cache key recalculates when plan changes
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:789` `cache.Key(cache.HashText(prompt), model, tuning)`
- **Notes:** Constraint is part of the rendered prompt; diffCacheKey hashes the full prompt, so a plan change yields a new key. No extra code needed.

### Criterion: AC6 — Oversized plan size-capped before injection
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/sprintplan.go:16` (MaxSprintPlanBytes=16384), `:68`, `:87-96` (capUTF8), resolveScopeConstraint truncation warning
- **Notes:** Content capped at 16 KiB on a UTF-8 rune boundary before wrapping; truncation surfaced on stderr. Cap applied before prepend so prompt cannot exceed payload_byte_budget.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — epic has no sprint-design.md)
**Files Reviewed:** 4 source files (sprintplan.go, fanout/review.go, cmd/atcr/review.go, fanout/resume.go) + tests
**Issues Found:** 8 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 8

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 4
- Low: 3

### Notable
- Adversarial agent confirmed capUTF8 has NO off-by-one and the 16384-byte boundary is correct (clean, not a finding).
- Finding #7 (test gaps) was narrowed: resolveScopeConstraint and prepend-ordering ARE tested; only the resume-path test and exact-byte truncation assertion are genuinely missing.
- All 6 acceptance criteria VERIFIED; the 8 findings are quality/robustness improvements, not AC failures. The epic is functionally complete and correct.

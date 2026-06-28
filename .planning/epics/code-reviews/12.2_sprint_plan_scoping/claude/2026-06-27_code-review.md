# Code Review Report: 12.2_sprint_plan_scoping

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 6 / 6 acceptance criteria
- **Approval Status:** Approved
- **Review Date:** June 27, 2026
- **Review Mode:** Epic (Acceptance Criteria + Adversarial + Tests)

All six acceptance criteria are fully implemented and verified against the merged code (epic merge `c546a41`). Tests pass (0 failures), total coverage is 89.0% (above the 80% baseline), and lint/types/format gates all pass. The adversarial pass surfaced 8 quality/robustness findings (0 critical, 1 high) — none of which is an AC failure; they are improvements for a follow-up TD pass.

## 2. Checklist Changes Applied
- **AC1** `[ ]` → `[x]` — `--sprint-plan` flag valid. Evidence: `cmd/atcr/review.go:54`, `:90-96`, `:234`
- **AC2** `[ ]` → `[x]` — missing/empty plan proceeds without warning. Evidence: `internal/payload/sprintplan.go:31-46`, `:63-67`
- **AC3** `[ ]` → `[x]` — unreadable plan warns on stderr, proceeds. Evidence: `internal/fanout/review.go` resolveScopeConstraint + stderr warn
- **AC4** `[ ]` → `[x]` — valid plan injects SCOPE CONSTRAINT before diff. Evidence: `internal/fanout/review.go:833` `Payload: scopeConstraint + mp.Text`
- **AC5** `[ ]` → `[x]` — cache key recalculates on plan change. Evidence: `internal/fanout/review.go:789`
- **AC6** `[ ]` → `[x]` — oversized plan size-capped. Evidence: `internal/payload/sprintplan.go:16,68,87-96`

## 3. Evidence Map
- **AC1 — CLI flag:** `cmd/atcr/review.go:54` registers `String("sprint-plan", ...)`; `sprintPlanPath()` trims and `runReview` maps to `ReviewRequest.SprintPlanPath`.
- **AC2 — missing/empty silent:** `ReadSprintPlan` returns `("",nil)` for empty path and `os.IsNotExist`; `ScopeConstraint("")` returns `("",false)`; `resolveScopeConstraint` returns `("","")` so no warn fires.
- **AC3 — unreadable warns:** non-NotExist read errors become a warning string; both prepare paths print `warn:` to stderr and proceed with an empty constraint.
- **AC4 — injection before diff:** constraint prepended to `{{.Payload}}` (rendered by every persona). `TestPrepareReviewFromDiff_InjectsSprintPlanConstraint` asserts the block precedes the diff in every slot.
- **AC5 — cache invalidation:** `diffCacheKey` hashes the full rendered prompt (`cache.Key(cache.HashText(prompt), ...)`); constraint is part of the prompt, so a plan change yields a new key.
- **AC6 — size cap:** `MaxSprintPlanBytes = 16384`; `capUTF8` cuts on a rune boundary and reports truncation; `resolveScopeConstraint` warns on truncation.

## 4. Remaining Unchecked Items
No remaining unchecked items — all 6 acceptance criteria verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** Implementation matches the epic's Technical Approach (prepend-to-Payload, template-agnostic), is well-documented, and is backed by focused unit + fanout tests. Minor deviation from plan (flag/helper live in `cmd/atcr/review.go` rather than a separate `flags.go`) is cleaner and functionally equivalent — not a defect.

## 6. Coverage Analysis
- **Coverage:** 89.0% (total); key packages: `internal/payload` 90.4%, `internal/fanout` 86.1%, `cmd/atcr` 83.8%
- **Baseline:** 80%
- **Delta:** ↑9.0%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... (gofmt -l clean) |

## 8. Adversarial Analysis
- **Files Reviewed:** 4 source files (+ tests), discovery-only mode (no sprint-design.md)
- **Issues Found:** 8 (Critical: 0, High: 1, Medium: 4, Low: 3)

### Issues by Severity
**HIGH**
- `internal/fanout/resume.go:287` (correctness) — Resume path silently drops the SCOPE CONSTRAINT: `PrepareResume` passes `""` and `cmd/atcr/resume.go` never sets `SprintPlanPath`, so a resumed scoped review reviews pending agents diff-wide, violating the "pending agents see exactly what completed agents reviewed" contract (`resume.go:265-267`).

**MEDIUM**
- `internal/payload/sprintplan.go:35` (error-handling) — `os.ReadFile` buffers the entire file before capping; no stat/size guard (unlike the diff path). A huge/non-regular path (e.g. `/dev/zero`) can hang/OOM and blow the <5ms NFR.
- `internal/fanout/review.go:833` (correctness) — AC6 holds only for the default 512 KiB budget; the hardcoded 16 KiB cap is added after `ApplyByteBudget`, so a small `--byte-budget` (e.g. 4096) is exceeded by the uncounted constraint.
- `internal/fanout/review.go:967` (maintainability) — Provenance gap: payload artifact records only `mp.Text`, not the injected constraint; nothing on disk records that the review was scoped / which plan / truncation.
- `internal/fanout/review.go:244` (maintainability) — Scope warnings hardcoded to global `os.Stderr`, contradicting the function's own doc and bypassing `log.FromContext(ctx)` used by the sibling diff-truncation warning; wrong for MCP/background callers.

**LOW**
- `internal/payload/sprintplan.go:77` (security) — Plan embedded verbatim between BEGIN/END markers with no marker-line escaping; a crafted plan could close the framing early (defense-in-depth only under the trusted-operator model).
- `internal/fanout/review_sprintplan_test.go:46` (testing) — Resume-path preservation untested (why the HIGH defect shipped); truncation test asserts only that a warning fires, not the exact byte count.
- `internal/fanout/review.go:833` (performance) — Constraint prepended once per agent and excluded from the byte budget → N×~16 KiB uncounted billed input per multi-agent review, invisible to an operator who set a budget to control spend.

### Confirmed clean (not findings)
- `capUTF8` has no off-by-one; the 16384-byte boundary and empty/whitespace handling are correct (independently verified).

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/12.2_sprint_plan_scoping.md` to merge these 8 findings into the TD README, then `/resolve-td` for the HIGH resume-path fix first.
- Consider prioritizing the resume-path constraint fix (HIGH) and the unbounded-read guard (MEDIUM) as they affect correctness/robustness.

---
*Generated by /execute-code-review on June 27, 2026 01:41:32PM*

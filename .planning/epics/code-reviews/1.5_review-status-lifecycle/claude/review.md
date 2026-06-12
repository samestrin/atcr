# Code Review Stream - 1.5_review-status-lifecycle (Epic)

**Started:** June 12, 2026 02:00:07PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: payload.Manifest carries effective timeout (timeout_secs, omitempty); old manifests still load and report status without stale inference
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/payload/manifest.go:39`, `internal/payload/manifest_test.go:111-127`
- **Notes:** `TimeoutSecs int json:"timeout_secs,omitempty"` present. Backward-compat tests TestManifest_TimeoutSecsTolerantWhenAbsent (absent → zero, no error) and TestManifest_TimeoutSecsOmittedWhenZero (omitempty round-trip). staleByDeadline returns false when TimeoutSecs<=0, so old manifests keep in_progress.

### Criterion: scaffolded review with no summary.json and StartedAt+timeout elapsed reports stale; within the window reports in_progress
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/status.go:148-156` (staleByDeadline), `internal/fanout/status.go:115-123` (read path), `internal/fanout/status_test.go:51-90`
- **Notes:** Tests TestReadReviewStatus_StaleWhenTimeoutElapsed, _InProgressWithinWindow, _StaleBoundaryIsExclusive (boundary exclusive via After), _NoTimeoutNeverStale, _ZeroStartedAtNeverStale, _CompletedNeverStale. Injectable nowFunc clock for determinism. Grace margin staleGraceSecs=60.

### Criterion: injected WritePool failure yields a state the reader reports as failed, not eternal in_progress
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/review.go:232-238`, `internal/fanout/artifacts.go:101-112` (writeFailureSummary), `internal/fanout/review_test.go:213-239`
- **Notes:** TestExecuteReview_WritePoolFailureMarksFailed forces WritePool error via invalid agent dir name, asserts RunFailed. Best-effort minimal summary (Total=roster, Failed=roster) → succeeded==0 → RunFailed via existing path. No new sentinel.

### Criterion: ReviewStatus JSON shape unchanged apart from new status value; MCP StatusResult alias and atcr status output stay compatible
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/mcp/handlers_test.go:417-432` (TestStatusHandler_Stale), `cmd/atcr/status_test.go:58-78` (TestStatusCmd_StaleJSON)
- **Notes:** StatusResult is a type alias of fanout.ReviewStatus; stale passes through both CLI and MCP unchanged. No struct field added — only the status string enum grows. Pure pass-through confirmed by tests.

### Criterion: read-path interpretation consistent with manifest-before-summary write ordering (no torn-pair misreport; concurrency test or documented invariant)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/status.go:74-94` (documented read-pair invariant), `internal/fanout/status_test.go:121-196` (TestReadReviewStatus_ConcurrentWritesNeverTornRead)
- **Notes:** Both invariant documentation AND a -race concurrency test (1 writer × 200 finalizations, 8 readers × 300 reads) asserting zero torn/corrupt/invalid observations. Manifest fields used by reader (roster, StartedAt, timeout_secs) byte-identical across finalization rewrite.

### Criterion: AC 04-04 contract amendment and skill polling guidance recorded
- **Verdict:** VERIFIED ✅
- **Evidence:** `.planning/plans/completed/1.0_atcr_core/acceptance-criteria/04-04-report-range-status-handlers.md:36-44, 88`, AC 05-03 orchestration-loop reference
- **Notes:** "Contract Amendment — stale status (Epic 1.5)" section added: additive enum, backward-compat, failed-vs-stale mapping, no new sentinel, poll-loop terminal guidance. Scenario 7 enum updated to include stale.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic mode)
**Files Reviewed:** 3 (status.go, artifacts.go, review.go)
**Issues Found:** 7 (verified from TD_STREAM)
**Risk Profile:** Not Available (discovery-only)

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 7

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 2
- Low: 5

### Notable findings
- MEDIUM status.go:122 — non-ErrNotExist read error on summary.json keeps reporting in_progress forever (the eternal-in_progress mode the epic targets, for the I/O-fault path).
- MEDIUM artifacts.go:109 — writeFailureSummary fabricates all-failed and discards real in-memory results, losing recoverable partial work.
- Agent confirmed several scrutinized concerns hold up: failure marker cannot clobber a valid summary.json (it is WritePool's final write); stale boundary logic correct (exclusive `After`); backward-compat guards sound; no path-traversal/perf regressions.

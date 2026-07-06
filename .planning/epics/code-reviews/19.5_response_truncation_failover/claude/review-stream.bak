# Code Review Stream - 19.5_response_truncation_failover (Epic)

**Started:** July 06, 2026 01:19:44PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

### Criterion: A reviewer response with finish_reason=length and zero parsed findings is recorded as StatusFailed, and the slot's fallback agent runs (FallbackUsed=true)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine.go:569-573` (invokeSlot demotes truncated + zero-parse to StatusFailed), `internal/fanout/review.go:571` (WithTruncationFailover wired into reviewer engine), tests `internal/fanout/response_truncation_test.go` (TestInvokeSlot_TruncatedZeroFindings_FailsAndFallsBack) + `response_truncation_e2e_test.go` (TestE2E_TruncatedZeroFindings_FailsOverAndRecords)
- **Notes:** Demotion sets Status=StatusFailed + errTruncatedZeroFindings so the existing Primary→Fallbacks chain descends; e2e test asserts FallbackUsed=true and the fallback's findings win.

### Criterion: A reviewer response with finish_reason=length and ≥1 parsed finding stays StatusOK, keeps its findings, and carries a truncated warning in the slot record / status.json
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine.go:569` (demotion guarded by zero-parse, so ≥1 finding stays OK), `internal/fanout/status.go:305` (AgentStatus.ResponseTruncated marker), `internal/fanout/artifacts.go:282` (statusFor sets it), tests TestInvokeSlot_TruncatedWithFindings_StaysOKWithMarker + TestE2E_TruncatedWithFindings_KeptWithMarkerInStatusJSON
- **Notes:** Partial findings preserved; response_truncated marker persisted to status.json.

### Criterion: An executor/fixer response that truncates with no usable patch fails over instead of returning a silent no-op success
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/executor.go:199-201` (truncated → non-silent FixWarning "fix generation truncated (finish_reason=length); no usable patch", partial text dropped), threaded via callExecutor + invokeExecutor, tests TestGenerateFixes_SnippetTruncated_FlagsNoUsablePatch + TestGenerateFixes_AgentModeTruncated_FlagsNoUsablePatch
- **Notes:** Per the epic Clarifications (executor has no cross-agent fallback chain), "fail over" = visible FixWarning, not a new mechanism. No usable patch is ever presented.

### Criterion: A per-agent truncated_zero_findings count is visible in status.json (or the run's telemetry) for observability
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/artifacts.go:45` (PoolSummary.TruncatedZeroFindings, json:truncated_zero_findings), `internal/fanout/artifacts.go:129-137` (run-level tally), `internal/fanout/status.go:305` (per-agent AgentStatus.ResponseTruncated), test TestWritePool_CountsTruncatedZeroFindings
- **Notes:** Per-agent marker + run-level count both in summary.json; demotion also emits a slog.Warn for per-event telemetry.

### Criterion: go test ./... passes, including new cases in internal/fanout/*_test.go and internal/verify/*_test.go covering the three scenarios
- **Verdict:** VERIFIED ✅ (test run in Phase 4)
- **Evidence:** New test files `internal/fanout/response_truncation_test.go`, `internal/fanout/response_truncation_e2e_test.go`, `internal/llmclient/truncation_test.go`, `internal/verify/executor_truncation_test.go`
- **Notes:** Confirmed green in Phase 4 quality gates below.

### Criterion: No behavior change for the normal finish_reason=stop path (regression-guarded by existing fanout tests)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/fanout/engine.go:363` (WithTruncationFailover is opt-in; executor engine leaves it off), `internal/llmclient/client.go:322` (Truncated only true on finish_reason=length; stop → false), test TestSingleShot_NoTruncationWhenClean + all pre-existing fanout tests pass
- **Notes:** finish_reason=stop yields Truncated=false, so the demotion guard never fires; CompleteWithUsage delegates to CompleteWithMeta preserving its four-value contract.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Full hostile review (2 parallel reviewers over 7 epic source files)
**Files Reviewed:** 7
**Issues Found:** 6 (verified from TD_STREAM)
**Risk Profile:** Not available (epic has no sprint-design.md)

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 2
- Low: 3

### Notable
- **HIGH (chunker.go:189)** — verified: `mergeResultGroup` inherits chunk[0]'s stale memoized `parsedFindingCount=0` after rebuilding merged Content, so a chunked persona whose first chunk truncated to zero findings silently drops all findings from its other chunks. Introduced by the **concurrent TD-019 memo refactor** (commits 1c36a190/361da806) present on this branch, not by the epic merge. Re-creates the exact silent-clean-review the epic prevents, for the chunked strategy.
- Both reviewers independently flagged the raw-parse (failover gate) vs grounded-count (telemetry tally) divergence — corroborating the MEDIUM already deferred during the epic's independent review.
- One MEDIUM is a genuine epic gap: `mergeResultGroup` never aggregates `ResponseTruncated` across chunks, so per-persona truncation telemetry is wrong for the chunked strategy.

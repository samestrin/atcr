# Code Review Stream - 3.1_verify_audit_attribution (Epic)

**Started:** June 14, 2026 09:50:53PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1 — invokeSkeptic returns trippedBudgets separately; verifyFinding populates base.TrippedBudgets from winning skeptic
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/invoke.go:45` (signature `(*reconcile.Verification, []string, error)`), `internal/verify/invoke.go:69` (returns `res.TrippedBudgets`), `internal/verify/pipeline.go:343` (`base.Model, base.TrippedBudgets = winningAttribution(...)`)
- **Notes:** Tripped budgets surfaced structurally, separate from Notes free-text. Clean verdict returns nil slice. Test: `invoke_test.go:162 TestInvokeSkeptic_SurfacesTrippedBudgets`, `invoke_test.go:176 TestInvokeSkeptic_NoTrippedBudgetsOnCleanVerdict`.

### Criterion: AC2 — VerificationResult.Model reflects winning skeptic's model (majority winner), not always skeptics[0]
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/pipeline.go:357` (`winningAttribution`): decisive vote records only skeptics whose verdict == winner; losers' models dropped. Tie records all participants.
- **Notes:** Test: `pipeline_test.go:330 TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute`, `pipeline_test.go:281 TestRunVerify_ThoroughMultiSkepticRecordsAllModels`, `pipeline_test.go:364 TestRunVerify_ThreeWayTieRecordsAllParticipantModels`.

### Criterion: AC3 — Model set explicitly on no_eligible_skeptic and tool_harness_unavailable paths (documented)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/pipeline.go:316` (`base.Model = ""` on no_eligible_skeptic) and `internal/verify/pipeline.go:325` (`base.Model = ""` on tool_harness_unavailable), each with documenting comment citing the epic clarification ("attribute only to what executed").
- **Notes:** AC text says "non-empty" but its own parenthetical and the recorded clarification (line 79) explicitly permit and require `""` on both pre-invocation paths. Code sets "" explicitly and documents it. Test: `pipeline_test.go:309 TestVerifyFinding_EarlyReturnsRecordEmptyModel`.

### Criterion: AC4 — skip-already-verified path carries Model/DurationMs/TrippedBudgets from on-disk verification.json
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/verify/pipeline.go:227` (lazy `loadPrior` reading `ReadVerificationResults`, keyed by FindingKey), `internal/verify/pipeline.go:266-272` (carry-forward with verdict-match guard), `internal/verify/emit_verification.go:114 ReadVerificationResults`.
- **Notes:** Exceeds AC: carry-forward guarded by verdict match so a stale/hand-edited prior cannot lend metadata to a different outcome. Lazy load avoids spurious warning when no finding skipped. Tests: `pipeline_test.go:430 TestRunVerify_SkipPreservesPriorMetadata`, `pipeline_test.go:397 TestRunVerify_SkipDropsMismatchedPriorMetadata`, `pipeline_test.go:624 TestRunVerify_CorruptPriorNoWarningWhenNoSkippedFindings`.

### Criterion: AC5 — existing pipeline tests pass; new tests cover 2-confirm/1-refute attribution and skip metadata preservation
- **Verdict:** VERIFIED ✅ (test execution confirmed in Phase 4)
- **Evidence:** `pipeline_test.go:330` (2-confirm/1-refute attribution), `pipeline_test.go:430` (skip metadata preservation) both present; plus `emit_test.go:406 TestReadVerificationResults`.
- **Notes:** Full pass/fail confirmed by `go test ./...` in Phase 4.

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md risk profile)
**Files Reviewed:** 7
**Issues Found:** 20 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 20

### Issues by Severity (verified)
- Critical: 0
- High: 2
- Medium: 7
- Low: 11

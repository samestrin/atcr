# Code Review Report: 3.1_verify_audit_attribution

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** June 14, 2026 09:50:53PM
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied
- **AC1** – invokeSkeptic returns trippedBudgets separately; verifyFinding populates base.TrippedBudgets from winning skeptic
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/verify/invoke.go:45`, `internal/verify/pipeline.go:343`
- **AC2** – VerificationResult.Model reflects winning skeptic's model (majority winner)
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/verify/pipeline.go:357`
- **AC3** – Model set explicitly ("") on no_eligible_skeptic and tool_harness_unavailable paths
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/verify/pipeline.go:316`, `internal/verify/pipeline.go:325`
- **AC4** – skip-already-verified path carries Model/DurationMs/TrippedBudgets from on-disk verification.json
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/verify/pipeline.go:227`, `internal/verify/emit_verification.go:114`
- **AC5** – existing tests pass; new tests cover 2-confirm/1-refute attribution and skip metadata preservation
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/verify/pipeline_test.go:330`, `internal/verify/pipeline_test.go:430`

## 3. Evidence Map
- **AC1 — tripped-budget threading**
  - Evidence: `internal/verify/invoke.go:45` (signature returns `[]string`), `internal/verify/invoke.go:69` (returns `res.TrippedBudgets` on trip, `nil` on clean verdict), `internal/verify/pipeline.go:343` (`winningAttribution` populates `base.TrippedBudgets`)
  - Summary: Tripped budgets surfaced structurally, separate from free-text Notes. Tested by `TestInvokeSkeptic_SurfacesTrippedBudgets` / `_NoTrippedBudgetsOnCleanVerdict`.
- **AC2 — winning-model attribution**
  - Evidence: `internal/verify/pipeline.go:357` (`winningAttribution`: decisive vote records winners only; tie records all participants)
  - Summary: Multi-vote Model is the deduped join of winners, never hard-coded to `skeptics[0]`. Tested by `TestRunVerify_WinningModelAttribution_TwoConfirmOneRefute`, `_ThreeWayTieRecordsAllParticipantModels`.
- **AC3 — empty Model on pre-invocation paths**
  - Evidence: `internal/verify/pipeline.go:316`, `:325` (both early returns set `base.Model = ""` with clarification-citing comments)
  - Summary: "Attribute only to what executed" — candidate-but-uninvoked skeptics contribute no model. Tested by `TestVerifyFinding_EarlyReturnsRecordEmptyModel`.
- **AC4 — skip-path metadata carry-forward**
  - Evidence: `internal/verify/pipeline.go:227` (lazy `loadPrior`), `:266` (carry-forward with verdict-match guard), `internal/verify/emit_verification.go:114` (`ReadVerificationResults`)
  - Summary: Skipped findings carry Model/DurationMs/TrippedBudgets from prior verification.json, guarded by verdict match. Tested by `TestRunVerify_SkipPreservesPriorMetadata`, `_SkipDropsMismatchedPriorMetadata`, `_CorruptPriorNoWarningWhenNoSkippedFindings`.
- **AC5 — test coverage**
  - Evidence: `internal/verify/pipeline_test.go:330`, `:430`, `internal/verify/emit_test.go:406`
  - Summary: All `internal/verify` tests pass; verify package coverage 93.3%.

## 4. Remaining Unchecked Items
No remaining unchecked items - all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All 5 acceptance criteria are satisfied with direct code evidence and dedicated tests. The implementation follows the recorded epic clarifications (AC3 empty-Model rule, winners-only attribution, verdict-matched carry-forward) and adds defensive behavior beyond the AC (verdict-match guard, lazy prior load). Full suite passes; lint/types/format clean. Findings below are non-blocking hardening/test-strengthening items.

## 6. Coverage Analysis
- **Coverage:** 87.9%
- **Baseline:** 80%
- **Delta:** ↑7.9%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Lint | PASSING | golangci-lint run |
| Types | PASSING | go vet ./... |
| Format | PASSING | go fmt ./... |

## 8. Adversarial Analysis
- **Files Reviewed:** 7
- **Issues Found:** 20 (Critical: 0, High: 2)

### Issues by Severity
**High (2)**
- `internal/verify/pipeline.go:72` — Stale docstring: claims SERIAL processing / "tracked as TD-009" while the body now fans out via a `MaxParallel` semaphore + goroutines. (maintainability)
- `internal/verify/pipeline_test.go:569` — `TestRunVerify_WorkerPoolPreservesOrder` uses an identical verdict for every finding, so it cannot detect a mis-ordered worker pool. (testing)

**Medium (7)**
- `internal/verify/pipeline.go:177` — No context-cancellation short-circuit in the fan-out; a cancelled run still dispatches every provider call. (error-handling)
- `internal/verify/pipeline.go:482` — `winningAttribution` indexes `skeptics[i]`/`perTripped[i]` without defending the alignment precondition; unguarded panic risk vs the stage's no-panic contract. (error-handling)
- `internal/verify/pipeline.go:472` — `winningAttribution` re-derives decisive/tie independently of `aggregateVerdicts`; the two can diverge on non-canonical verdicts. (maintainability)
- `internal/verify/pipeline_test.go:281` — `_ThoroughMultiSkepticRecordsAllModels` (all-confirm) asserts only substring Contains; no winner-vs-loser discrimination, no exact string. (testing)
- `internal/verify/pipeline_test.go:355` — AC2 test uses fragile substring Contains/NotContains instead of exact equality. (testing)
- `internal/verify/pipeline_test.go:330` — AC2's 2-refute/1-confirm polarity is untested. (testing)
- `internal/verify/invoke_test.go:162` — AC1 structural surfacing proven only for `max_turns`; `tool_budget_bytes`/`timeout_secs` not asserted on the returned slice. (testing)

**Low (11)** — programming-fault branch lacks logging; verifyFinding comment half-true about winners-only; tripped-budget slice aliases engine memory undocumented; log-injection via unsanitized stderr detail; live DurationMs never asserted non-zero; no live confirmed+budget-trip combined test; non-FC degrade untested under multi-vote; alphabetical-ordering assumption unasserted; stderr-pipe deadlock risk in two tests; skip+corrupt-prior drop case untested; `DegradesWhenNotFC` assertion satisfied by too many failure modes. (See `td-stream.txt` for full detail.)

## 9. Follow-ups
- Run `/reconcile-code-review @.planning/epics/completed/3.1_verify_audit_attribution.md` to merge these 20 findings into the technical-debt README with reviewer attribution.
- Consider fixing the 2 HIGH items (stale concurrency docstring; non-discriminating worker-pool test) — both are low-effort (15/30 min) and improve the trustworthiness of the verify stage's docs and tests.

---
*Generated by /execute-code-review on June 14, 2026 09:50:53PM*

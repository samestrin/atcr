# Code Review Report: 15.1_leaderboard-cost-na-rendering

## 1. Executive Summary
- **Overall Result:** Pass
- **Items Checked:** 5 / 5
- **Approval Status:** Approved
- **Review Date:** July 02, 2026 10:31:37AM
- **Review Mode:** Epic (Acceptance Criteria + Adversarial) + Tests

## 2. Checklist Changes Applied

- **`.planning/epics/completed/15.1_leaderboard-cost-na-rendering.md`** – AC1: `scorecard.PublicRecord` supports an explicit N/A/absent state
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/scorecard/export.go:42`
- **`.planning/epics/completed/15.1_leaderboard-cost-na-rendering.md`** – AC2: Production export emits the new N/A state when `cost > 0 && corroborated == 0`
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/scorecard/export.go:272-281`, `internal/scorecard/export_test.go:113-131`
- **`.planning/epics/completed/15.1_leaderboard-cost-na-rendering.md`** – AC3: Benchmark run emits the identical N/A state for the same condition
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/benchmark/score.go:112-121`, `internal/benchmark/score_test.go:83-93`
- **`.planning/epics/completed/15.1_leaderboard-cost-na-rendering.md`** – AC4: All existing scorecard and benchmark tests are updated and pass
  - Before: `[ ]` → After: `[x]`
  - Evidence: `internal/scorecard/export_test.go`, `internal/benchmark/score_test.go`, `cmd/atcr/benchmark_run_test.go:80-81`, `cmd/atcr/benchmark_run_resume_test.go:448-449,495-496`
- **`.planning/epics/completed/15.1_leaderboard-cost-na-rendering.md`** – AC5: `docs/scorecard.md` and `docs/benchmark.md` field-reference tables document the new absent/N/A representation
  - Before: `[ ]` → After: `[x]`
  - Evidence: `docs/scorecard.md:242`, `docs/benchmark.md:128`

## 3. Evidence Map

- **AC1: Explicit N/A/absent state**
  - Evidence: `internal/scorecard/export.go:42`
  - Summary: `CostPerCorroboratedFindingUSD` changed from `float64` to `*float64` with `json:"cost_per_corroborated_finding_usd,omitempty"`, mirroring the `SurvivedSkepticRate` precedent on the same struct.
- **AC2: Production export N/A state**
  - Evidence: `internal/scorecard/export.go:272-281`, `internal/scorecard/export_test.go:113-131`
  - Summary: `costPer` returns `nil` when `corroborated <= 0`; tests assert both the JSON-key-absent case (paid, uncorroborated) and the JSON-key-present-at-0.0 case (genuinely free with corroborated findings) by inspecting the raw JSON string.
- **AC3: Benchmark N/A state**
  - Evidence: `internal/benchmark/score.go:112-121`, `internal/benchmark/score_test.go:83-93`
  - Summary: `scoreOne`'s guard sets the pointer only when `matchedFindings > 0`; `TestScore_CostPerCorroboratedNilWhenPaidButUnmatched` locks in the nil case.
- **AC4: Tests updated and passing**
  - Evidence: 6 updated call sites in `export_test.go`, 3 updated + 1 new in `score_test.go`, downstream consumer tests in `cmd/atcr/`
  - Summary: `go test ./...` (see Section 7) passes across all packages with 0 failures.
- **AC5: Docs updated**
  - Evidence: `docs/scorecard.md:242`, `docs/benchmark.md:128`
  - Summary: Both field-reference tables changed from "always" to "omitempty" with full disambiguation prose distinguishing "omitted = undefined" from "present at 0.0 = genuinely free."

## 4. Remaining Unchecked Items

No remaining unchecked items - all verified.

## 5. Manual Review Status
- **Code Reviewed and Approved:** Checked
- **Rationale:** All 5 acceptance criteria verified against the merged implementation (commit `ce0853a3`), full test suite passes, coverage exceeds baseline, lint/types/format clean, and adversarial review found no critical or blocking issues in the epic's core logic.

## 6. Coverage Analysis
- **Coverage:** 89.3%
- **Baseline:** 80%
- **Delta:** ↑9.3%
- **Status:** PASSING

## 7. Quality Checks
| Check | Status | Command |
|-------|--------|---------|
| Tests | PASSING | `go test ./...` |
| Lint | PASSING | `golangci-lint run` |
| Types | PASSING | `go vet ./...` |
| Format | PASSING | `go fmt ./...` |

## 8. Adversarial Analysis
- **Files Reviewed:** 8 (internal/scorecard/export.go, internal/scorecard/export_test.go, docs/scorecard.md, internal/benchmark/score.go, internal/benchmark/score_test.go, cmd/atcr/benchmark_run_test.go, cmd/atcr/benchmark_run_resume_test.go, docs/benchmark.md)
- **Issues Found:** 10 (Critical: 0, High: 1)

### Issues by Severity

**HIGH (1)**
- `cmd/atcr/benchmark_run_resume_test.go:269-302` — pre-existing test leaks an `os.Stderr` swap if the resumed run errors before restoration runs, corrupting diagnostics for the rest of the test process. (pre-existing, surfaced while reviewing a file this epic modified)

**MEDIUM (4)**
- `docs/scorecard.md:292-294` — the "Preserved (allowlist)" doc summary wasn't updated in parallel with the field-reference table this epic changed; doesn't flag the new omitempty behavior. (sprint, directly relevant to AC5's intent)
- `internal/scorecard/export.go:275-281` — the documented "never Inf/NaN" guarantee on `costPer` isn't enforced against an accumulator overflow. (pre-existing)
- `internal/benchmark/score.go:120-123` — `scoreOne` divides by `matchedFindings` without validating `CostUSD` for negative/NaN/Inf. (pre-existing)
- `cmd/atcr/benchmark_run_test.go:141-152` — the CLI round-trip integration test's decode struct omits `cost_per_corroborated_finding_usd`, so it doesn't cover the epic's core omit-vs-0.0 distinction at the CLI boundary. (sprint)

**LOW (5)**
- `internal/scorecard/export_test.go:115-143` — no test covers group-aggregated (2+ record) corroboration totals for the nil/present distinction.
- `internal/scorecard/export.go:275-278` — `costPer`'s `<= 0` guard is misleading since `corroborated` can never be negative.
- `internal/scorecard/export.go:229-231` — scrub-to-empty on path-like reviewer identities can silently merge distinct groups (adjacent, not caused by this epic).
- `internal/benchmark/score.go:91-110` — redundant `normalize()` calls in the matched-findings loop.
- `internal/benchmark/score.go:38-40` — documented-but-unenforced empty-`Expected` footgun.

**Assessment:** The core epic change — the `*float64` + `omitempty` schema change, `costPer`'s nil-return semantics, and the production/benchmark wiring — is correctly implemented and directly covered by targeted tests for the free-vs-uncorroborated distinction this epic exists to fix. All 10 adversarial findings are either pre-existing edge cases surfaced while reviewing the touched files, or minor documentation/test-coverage gaps at the margins. None block the acceptance criteria; all have been routed to `.planning/.temp/execute-code-review-claude/td-stream.txt` for `/reconcile-code-review`.

## 9. Follow-ups

Run `/reconcile-code-review @.planning/epics/completed/15.1_leaderboard-cost-na-rendering.md` to merge these findings into `.planning/technical-debt/README.md`.

---
*Generated by /execute-code-review on July 02, 2026 10:31:37AM*

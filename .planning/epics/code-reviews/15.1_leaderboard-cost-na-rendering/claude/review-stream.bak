# Code Review Stream - 15.1_leaderboard-cost-na-rendering (Epic)

**Started:** July 02, 2026 10:31:37AM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests if enabled]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion: AC1: `scorecard.PublicRecord` supports an explicit N/A/absent state for `cost_per_corroborated_finding_usd`.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/export.go:42` (`CostPerCorroboratedFindingUSD *float64 \`json:"cost_per_corroborated_finding_usd,omitempty"\``)
- **Notes:** Field changed from plain `float64` to `*float64` with `omitempty`, mirroring the `SurvivedSkepticRate` precedent on the same struct.

### Criterion: AC2: Production export emits the new N/A state when `cost > 0 && corroborated == 0`.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/export.go:272-281` (`costPer` returns `nil` when `corroborated <= 0`); `internal/scorecard/export_test.go:113-131` (`TestExport_CostPerCorroboratedAbsentWhenNoCorroboration` asserts the JSON key is absent when paid but uncorroborated, and `TestExport_CostPerCorroboratedPresentAndZeroWhenGenuinelyFree` asserts the key is present at `0.0` when genuinely free with corroborated findings).
- **Notes:** Both disambiguating cases are covered by dedicated tests reading the raw JSON string, not just the parsed struct.

### Criterion: AC3: Benchmark run emits the identical N/A state for the same condition.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/benchmark/score.go:112-121` (guard sets `pr.CostPerCorroboratedFindingUSD = &v` only when `matchedFindings > 0`); `internal/benchmark/score_test.go:83-93` (`TestScore_CostPerCorroboratedNilWhenPaidButUnmatched` asserts nil when priced but zero matched categories).
- **Notes:** Matches production semantics exactly — nil when unmatched, real pointer (including 0.0) otherwise.

### Criterion: AC4: All existing scorecard and benchmark tests are updated and pass.
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/scorecard/export_test.go` (6 call sites updated to pointer-deref pattern), `internal/benchmark/score_test.go` (3 call sites updated + 1 new test), `cmd/atcr/benchmark_run_test.go:80-81`, `cmd/atcr/benchmark_run_resume_test.go:448-449,495-496` (downstream consumers updated for the pointer type change).
- **Notes:** `go test ./...` run in Phase 4 confirms all pass (see Quality Checks section).

### Criterion: AC5: `docs/scorecard.md` and `docs/benchmark.md` field-reference tables document the new absent/N/A representation for `cost_per_corroborated_finding_usd`.
- **Verdict:** VERIFIED ✅
- **Evidence:** `docs/scorecard.md:242` (row changed from "always" to "**omitempty**" with full disambiguation prose), `docs/benchmark.md:128` (row updated to describe "Omitted from the JSON entirely" semantics).
- **Notes:** Both docs clearly distinguish "omitted = undefined" from "present at 0.0 = genuinely free/zero-cost."

## Test and Quality Results

- **Tests:** PASSING (all packages, 0 failures)
- **Coverage:** 89.3% (baseline 80%, delta ↑9.3%)
- **Lint:** PASSING (golangci-lint: 0 issues)
- **Types:** PASSING (go vet: clean)
- **Format:** PASSING (gofmt clean on all epic-relevant files; 2 unrelated pre-existing spike files under .planning/.temp/spike-2.0/ are unformatted but out of scope)

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (no sprint-design.md — epic mode)
**Files Reviewed:** 6 (internal/scorecard/export.go, internal/scorecard/export_test.go, docs/scorecard.md, internal/benchmark/score.go, internal/benchmark/score_test.go, cmd/atcr/benchmark_run_test.go, cmd/atcr/benchmark_run_resume_test.go, docs/benchmark.md)
**Issues Found:** 10 (verified from TD_STREAM)
**Risk Profile:** Not Available (epic mode has no sprint-design.md)

### Issues by Severity (verified)
- Critical: 0
- High: 1
- Medium: 4
- Low: 5

### Notable findings
- The core epic change (`*float64` + `omitempty` wiring, `costPer` nil-return, `finalize()`) is correctly implemented and well covered by tests for the specific nil-vs-zero distinction — no defect found in the central logic.
- HIGH: `cmd/atcr/benchmark_run_resume_test.go:269-302` — pre-existing test leaks a stderr-swap on early test failure (unrelated to this epic's change, but touches a file this epic modified).
- MEDIUM: `docs/scorecard.md:292-294` — the "Preserved (allowlist)" summary was not updated in parallel with the field-reference table this epic changed, so it doesn't mention the new omitempty behavior (directly relevant to AC5's intent).
- Remaining MEDIUM/LOW findings are pre-existing edge cases (Inf/NaN propagation, test coverage gaps, minor maintainability nits) surfaced while reviewing the same code paths — none block the epic's acceptance criteria.

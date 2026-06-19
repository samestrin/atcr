# Code Review Stream - 4.2_config_validation (Epic)

**Started:** June 18, 2026 08:11:28PM
**Mode:** [Acceptance Criteria] [+ Adversarial Review] [+ Tests]

---

## Acceptance Criteria Findings

<!-- Findings appended immediately as discovered -->

### Criterion AC1: Missing required field (provider/model) reported
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:330-336` (validateAgent: required 'provider'/'model'); test `internal/registry/accumulate_test.go:200` (TestLoadRegistry_ReportsAllErrorsAtOnce)
- **Notes:** Epic's literal `registry.default_provider: required` is illustrative (per Clarifications). Real model uses `agent '<name>': required field 'model'/'provider'`. Intent satisfied.

### Criterion AC2: Invalid payload_mode enum reported
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:218` (`invalid payload_mode '%s'`); agent-level at validateAgent; tested in accumulate_test + command test.
- **Notes:** Also covers min_severity enum. Message lists valid set.

### Criterion AC3: Out-of-range timeout reported
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:209` (`timeout_secs must be within 1..%d`). Asserted in TestLoadRegistry_ReportsAllErrorsAtOnce and command test.
- **Notes:** Field is `timeout_secs` (real) not `registry.timeout` (illustrative). Intent satisfied.

### Criterion AC4: min_severity + max_findings=0 nonsensical case
- **Verdict:** VERIFIED ✅ (intent)
- **Evidence:** `internal/registry/config.go` validateAgent `max_findings must be within 1..%d` (rejects 0 outright); min_severity enum check.
- **Notes:** Literal case moot — max_findings:0 already rejected. Accumulation ensures it co-reports with siblings (per Clarifications).

### Criterion AC5: Validation errors returned as usageError (exit code 2)
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review_test.go:TestReviewCmd_InvalidConfigReportsAllErrors` asserts `require.Equal(t, 2, code)`.

### Criterion AC6: All validation errors reported at once (CORE DELIVERABLE)
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/config.go:206-243` (validate accumulates into errors.Join); `graph.go:31-70` (ValidateFallbacks accumulates); `attribution.go:55-63` (join-aware attribution). Tests: TestValidate_AccumulatesAllErrors, TestValidateFallbacks_AccumulatesMultipleDangling/DanglingAndCycle/TwoIndependentCycles.
- **Notes:** Deterministic ordering via `sortedKeys` (TestValidate_DeterministicOrder). See adversarial finding re: cross-function short-circuit in validateMerged/LoadRegistry.

### Criterion AC7: Validation runs before API calls / review directory creation
- **Verdict:** VERIFIED ✅
- **Evidence:** Validation at config load (`config.go:193-197` LoadRegistry; `overlay.go:142` validateMerged). Command test fails with exit 2 before execution.
- **Notes:** Command test asserts exit code + message but does not explicitly assert no review directory was created (minor coverage gap).

### Criterion AC8: go test ./internal/registry/... covers each category
- **Verdict:** VERIFIED ✅
- **Evidence:** `internal/registry/accumulate_test.go` — covers required-field, enum, type/range, semantic, fallback dangling/cycle, attribution, happy path.

### Criterion AC9: Integration test — atcr review with invalid config fails fast
- **Verdict:** VERIFIED ✅
- **Evidence:** `cmd/atcr/review_test.go:TestReviewCmd_InvalidConfigReportsAllErrors` (full command path via execCmdCapture).

## Adversarial Analysis (Risk Verification Mode)

**Mode:** Verification + Discovery (discovery-only — no sprint-design.md for epic)
**Files Reviewed:** 5 (config.go, graph.go, attribution.go, accumulate_test.go, review_test.go)
**Issues Found:** 2 (verified from TD_STREAM)
**Risk Profile:** Not Available

### Risk Verification Summary
- ✅ Anticipated & Addressed: 0
- ⚠️ Anticipated & Missed: 0
- 🔍 Unanticipated: 2

### Issues by Severity (verified)
- Critical: 0
- High: 0
- Medium: 0
- Low: 2

### Notable strengths
- `else if` conversions in validateProvider/validateAgent prevent spurious cascading faults (e.g. "references unknown provider ''") that naive accumulation would emit.
- `walkFallbacks` blackens all visited nodes on cycle detection; safe under the single-outgoing-edge invariant and regression-tested against both reviewer repros (TestValidateFallbacks_LeadInLeftGrayThenRevisited).
- `attribute()` recurses over errors.Join trees so each fault is prefixed with its own defining file (project vs user tier).
- Deterministic ordering via `sortedKeys` is explicitly tested across 20 runs.

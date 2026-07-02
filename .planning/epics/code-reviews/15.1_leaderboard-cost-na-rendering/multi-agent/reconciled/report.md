# atcr Reconciled Review

## Summary

- Total findings: 5
- Sources: pool
- Clusters collapsed: 0
- Severity disagreements: 0
- Partial: yes (a source was missing or unreadable)

| Severity | HIGH conf | MEDIUM conf | LOW conf |
|----------|-----------|-------------|----------|
| CRITICAL | 0 | 0 | 0 |
| HIGH | 0 | 1 | 0 |
| MEDIUM | 0 | 2 | 0 |
| LOW | 0 | 2 | 0 |

## Disagreements

Top 5 tension spot(s) — reviewer splits, solo findings, and gray-zone clusters, highest first.

### 1. solo_finding — `internal/scorecard/export_test.go:381` (HIGH) · score 3
- Reviewers: dax (independence 1)
- Problem: TestExport_ClampsNegativeMetrics sets FindingsCorroborated=-2 which clamps to 0 at ingestion, so CostPerCorroboratedFindingUSD is always nil and the nil-guard assertion on that field is dead code, never exercising negative-CostUSD clamping through the non-nil pointer path.

### 2. solo_finding — `internal/benchmark/score.go:115` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: scoreOne now sets CostPerCorroboratedFindingUSD to a *float64 when matchedFindings&gt;0, but the zero-cost-with-matches path (CostUSD==0, matchedFindings&gt;0) yields a non-nil pointer to 0.0 — the benchmark scorer has no test asserting this specific disambiguation case (free+matched vs paid+unmatched).

### 3. solo_finding — `internal/scorecard/export.go:99` (MEDIUM) · score 2
- Reviewers: dax (independence 1)
- Problem: reviewerAcc.finalize() always calls costPer and assigns the result, but costPer now returns nil when corroborated==0 — the nil pointer is assigned and omitempty drops the key, which is correct, but there is no test asserting the nil-assignment path through finalize (the only test of the nil case goes through Export→parseEnvelope, not the accumulator directly).

### 4. solo_finding — `.planning/technical-debt/README.md:67` (LOW) · score 1
- Reviewers: otto (independence 1)
- Problem: TD item for &#96;internal/scorecard/export_test.go:381&#96; is listed as &#96;LOW&#96; but describes a dead-code/test-gap scenario that could mask regressions in clamping logic

### 5. solo_finding — `internal/scorecard/export_test.go:103` (LOW) · score 1
- Reviewers: dax (independence 1)
- Problem: TestExport_CostPerCorroboratedFinding uses a fixed record with FindingsCorroborated=7 and CostUSD=0.04 — the test name and assertion verify the division but the record&#39;s FindingsCorroborated is hardcoded in exportRec, making the test fragile to changes in the helper.

## Findings

### HIGH

- `internal/scorecard/export_test.go:381` — confidence MEDIUM, reviewers: dax
  - Problem: TestExport_ClampsNegativeMetrics sets FindingsCorroborated=-2 which clamps to 0 at ingestion, so CostPerCorroboratedFindingUSD is always nil and the nil-guard assertion on that field is dead code, never exercising negative-CostUSD clamping through the non-nil pointer path.
  - Fix: Add a second case with positive FindingsCorroborated and negative CostUSD so the clamp is exercised through the non-nil pointer branch.
  - Evidence: rec.FindingsCorroborated = -2; if r.CostPerCorroboratedFindingUSD != nil { assert.GreaterOrEqual(t, *r.CostPerCorroboratedFindingUSD, 0.0) } — the nil-guard is always taken, the GreaterOrEqual assertion never executes.

### MEDIUM

- `internal/benchmark/score.go:115` — confidence MEDIUM, reviewers: dax
  - Problem: scoreOne now sets CostPerCorroboratedFindingUSD to a *float64 when matchedFindings&gt;0, but the zero-cost-with-matches path (CostUSD==0, matchedFindings&gt;0) yields a non-nil pointer to 0.0 — the benchmark scorer has no test asserting this specific disambiguation case (free+matched vs paid+unmatched).
  - Fix: Add a test case: CostUSD=0, matchedFindings&gt;0 → assert NotNil and *v==0.0.
  - Evidence: TestScore_CostPerCorroboratedNilWhenPaidButUnmatched covers the nil path but no test covers the non-nil-zero path.
- `internal/scorecard/export.go:99` — confidence MEDIUM, reviewers: dax
  - Problem: reviewerAcc.finalize() always calls costPer and assigns the result, but costPer now returns nil when corroborated==0 — the nil pointer is assigned and omitempty drops the key, which is correct, but there is no test asserting the nil-assignment path through finalize (the only test of the nil case goes through Export→parseEnvelope, not the accumulator directly).
  - Fix: Add a unit test calling finalize on an acc with corroborated==0 and asserting CostPerCorroboratedFindingUSD is nil.
  - Evidence: pr.CostPerCorroboratedFindingUSD = costPer(a.costTotal, a.corroborated) — no test of finalize with corroborated==0 directly.

### LOW

- `.planning/technical-debt/README.md:67` — confidence MEDIUM, reviewers: otto
  - Problem: TD item for &#96;internal/scorecard/export_test.go:381&#96; is listed as &#96;LOW&#96; but describes a dead-code/test-gap scenario that could mask regressions in clamping logic
  - Fix: Update severity to MEDIUM or refine the &#34;Problem&#34; to emphasize the risk of missing regressions
  - Evidence: &#96;TestExport_ClampsNegativeMetrics&#96; sets FindingsCorroborated=-2 which clamps to 0... assertion on that field dead code&#96;
- `internal/scorecard/export_test.go:103` — confidence MEDIUM, reviewers: dax
  - Problem: TestExport_CostPerCorroboratedFinding uses a fixed record with FindingsCorroborated=7 and CostUSD=0.04 — the test name and assertion verify the division but the record&#39;s FindingsCorroborated is hardcoded in exportRec, making the test fragile to changes in the helper.
  - Fix: Extract the expected value from the record&#39;s own fields (0.04/7.0) or document the dependency on exportRec&#39;s internals.
  - Evidence: exportRec(&#34;bruce&#34;, &#34;claude-sonnet-4-6&#34;, 1) — FindingsCorroborated=7 is an implicit contract of exportRec.

# Acceptance Criteria: Aggregate Run Record

**Related User Story:** [01: Auto-emit Scorecard](../user-stories/01-auto-emit-scorecard.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Aggregate Builder | Go function in `internal/scorecard` | Summarizes all reviewers |
| Record Type | Distinct `record_type` field | `"reviewer"` vs `"aggregate"` |
| Test Framework | `go test` + `testify` | Unit test with multi-reviewer fixture |

## Related Files
- `internal/scorecard/scorecard.go` - modify: aggregate record builder (sum findings, avg rate, total cost)
- `internal/scorecard/aggregate.go` - create: aggregation logic (sum, average, totals)
- `internal/scorecard/store.go` - modify: emit aggregate record after per-reviewer records

## Happy Path Scenarios
**Scenario 1: Aggregate record appended after per-reviewer records**
- **Given** reconcile run with 3 reviewers
- **When** scorecard emission completes
- **Then** JSONL file has 4 new lines: 3 reviewer records + 1 aggregate record (last line)

**Scenario 2: Aggregate record sums findings across reviewers**
- **Given** reviewer-A raised 5 findings, reviewer-B raised 3, reviewer-C raised 2
- **When** aggregate record is built
- **Then** aggregate `findings_raised: 10`, `findings_corroborated` = sum of all, `findings_solo` = sum of all

**Scenario 3: Aggregate record totals cost and tokens**
- **Given** reviewer-A cost $0.05, reviewer-B cost $0.03, reviewer-C cost $0.02
- **When** aggregate record is built
- **Then** aggregate `cost_usd: 0.10`, `tokens_in` and `tokens_out` are sums

**Scenario 4: Aggregate record has record_type="aggregate"**
- **Given** any reconcile run
- **When** aggregate record is serialized
- **Then** JSON contains `"record_type": "aggregate"`; per-reviewer records contain `"record_type": "reviewer"`

## Edge Cases
**Edge Case 1: Single reviewer run**
- **Given** reconcile with only 1 reviewer
- **When** records are emitted
- **Then** 2 records written: 1 reviewer + 1 aggregate; aggregate values equal the single reviewer's values

**Edge Case 2: Zero reviewers (edge case — should not happen but guard)**
- **Given** reconcile Result with empty reviewer list
- **When** emission is attempted
- **Then** only aggregate record written with zeroed fields (or emission skipped with warning)

**Edge Case 3: Aggregate corroboration rate**
- **Given** multiple reviewers with different corroboration rates
- **When** aggregate rate is computed
- **Then** rate is computed from totals (total_corroborated / total_raised), not average of rates

## Error Conditions
**Error Scenario 1: Aggregate computation overflow**
- Error message: N/A — use float64 for cost/rate; int64 for token counts; overflow practically impossible at run scale

## Performance Requirements
- **Response Time:** Aggregation across ≤ 20 reviewers completes < 1ms
- **Throughput:** One additional record appended per run

## Security Considerations
- **Input Validation:** Aggregate values derived from already-validated per-reviewer data
- **Data Protection:** Same local-only storage as per-reviewer records

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Multi-reviewer fixture (3+ reviewers) with known finding counts, costs, tokens
**Mock/Stub Requirements:** None — pure computation

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Aggregate record is last line in each run's emission batch
- [ ] `record_type` field distinguishes `"reviewer"` from `"aggregate"`
- [ ] Sums and totals verified against manual calculation
- [ ] Corroboration rate computed from totals, not averaged

**Manual Review:**
- [ ] Code reviewed and approved

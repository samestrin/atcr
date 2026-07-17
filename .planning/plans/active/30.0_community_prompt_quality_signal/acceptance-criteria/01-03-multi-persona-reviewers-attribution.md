# Acceptance Criteria: Multi-Persona `Reviewers` Attribution Rule

**Related User Story:** [01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters](../user-stories/01-aggregate-per-persona-model-dismissal-counters.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go aggregation logic (`internal/localdebt`) | Attribution rule inside `AggregateQualitySignal` (AC 01-01) |
| Test Framework | Go `testing` (table-driven) | Fixture with a 2+-reviewer merged record, per Story's own risk table |
| Key Dependencies | None new | Reuses `Record.Reviewers []string` already present |

## Related Files
- `internal/localdebt/qualitysignal.go` - modify (depends on AC 01-01): iterate every entry in `Record.Reviewers` (not `Reviewers[0]`) and attribute one dismissed/confirmed outcome to each listed persona's `(persona, model)` group for that record
- `internal/localdebt/qualitysignal_test.go` - modify (depends on AC 01-01): add a fixture record with `Reviewers: []string{"security-reviewer", "perf-reviewer"}` and `Status: "wontfix"`, asserting both personas' groups receive `DismissedCount += 1` from that single record
- `internal/reconcile/merge.go` - reference only: `unionReviewers` is the upstream source of multi-persona `Reviewers` lists this attribution rule must handle correctly
- `internal/localdebt/record.go` - reference only: `Record.Reviewers []string` field this rule reads

## Happy Path Scenarios
**Scenario 1: Single-reviewer record attributes to one persona**
- **Given** a record with `Reviewers: []string{"security-reviewer"}`, `Model: "claude-sonnet-4-6"`, `Status: "resolved"`
- **When** aggregated
- **Then** exactly the `("security-reviewer", "claude-sonnet-4-6")` group's `ConfirmedCount` increments by 1, and no other group is affected

**Scenario 2: Multi-reviewer merged record attributes to every listed persona**
- **Given** a record with `Reviewers: []string{"security-reviewer", "perf-reviewer"}`, `Model: "claude-sonnet-4-6"`, `Status: "wontfix"`
- **When** aggregated
- **Then** both `("security-reviewer", "claude-sonnet-4-6")` and `("perf-reviewer", "claude-sonnet-4-6")` groups' `DismissedCount` each increment by 1 from this single record — the outcome is attributed to every listed persona, not just the first

## Edge Cases
**Edge Case 1: Empty `Reviewers` slice**
- **Given** a record with `Reviewers: []string{}` (or `nil`)
- **When** aggregated
- **Then** the record contributes to no persona group at all (it cannot be attributed) and is silently excluded rather than causing an error or an empty-persona-string group

**Edge Case 2: `Reviewers` contains a duplicate or empty-string entry**
- **Given** a record with `Reviewers: []string{"security-reviewer", "", "security-reviewer"}`
- **When** aggregated
- **Then** the empty string is skipped, and `security-reviewer`'s count increments by exactly 1 for this record (not 2) — de-duplicated per-record attribution, matching `unionReviewers`'s own dedup discipline upstream

**Edge Case 3: Three or more reviewers on one record**
- **Given** a record with `Reviewers` holding 3+ persona names (a larger multi-agent merge)
- **When** aggregated
- **Then** every listed persona's group receives the increment — the rule generalizes to N reviewers, not just 2

## Error Conditions
**Error Scenario 1: Not applicable**
- Attribution is a pure in-memory fold with no error path; malformed/empty entries are skipped per the edge cases above rather than raising an error.

## Performance Requirements
- **Response Time:** A record with k reviewers contributes O(k) group updates; k is small in practice (bounded by the number of personas configured in a fan-out run), so this does not change the overall O(n) complexity of AC 01-01's aggregation.
- **Throughput:** No additional I/O; purely in-memory fold.

## Security Considerations
- **Authentication/Authorization:** Not applicable.
- **Input Validation:** No content beyond persona name strings (already-known catalog identifiers, not user-supplied free text) is read from `Reviewers`; no finding content is touched.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture records covering: single reviewer, 2 reviewers, 3+ reviewers, empty `Reviewers`, and a `Reviewers` list with a duplicate/empty entry.
**Mock/Stub Requirements:** None — pure in-memory function under test.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/localdebt/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A record with 2+ `Reviewers` increments every listed persona's group, not just `Reviewers[0]`
- [ ] An empty `Reviewers` slice contributes to no group
- [ ] Duplicate/empty entries within one record's `Reviewers` are deduplicated per-record (no double-count)

**Manual Review:**
- [ ] Code reviewed and approved

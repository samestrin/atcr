# Acceptance Criteria: Per-(Persona, Model) Dismissed/Confirmed Aggregation

**Related User Story:** [01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters](../user-stories/01-aggregate-per-persona-model-dismissal-counters.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package function (`internal/localdebt`) | New aggregation function, sibling of `internal/scorecard/aggregate.go`'s `Aggregate()` |
| Test Framework | Go `testing` (table-driven) | Mirrors `internal/scorecard/aggregate_test.go` conventions |
| Key Dependencies | `internal/localdebt.ReadAll`, `sort` (stdlib) | No new third-party dependency |

## Related Files
- `internal/localdebt/qualitysignal.go` - create: new `AggregateQualitySignal(records []Record) []QualityRow` function grouping folded terminal records by `(persona, model)` and summing `DismissedCount`/`ConfirmedCount`
- `internal/localdebt/qualitysignal_test.go` - create: table-driven fixture test asserting exact per-group counts and deterministic row order
- `internal/scorecard/aggregate.go` - reference only: the `Aggregate()` grouping/sort idiom (map-of-key + insertion-order slice + `sort.SliceStable` tie-break) this function must structurally mirror
- `internal/localdebt/record.go` - reference only: `Record.Reviewers []string`, `Record.Status`, and the new `Model` field (Story 1 AC 01-02) this function reads

### Related Files (from codebase-discovery.json)

- `internal/localdebt/qualitysignal.go` - create: `AggregateQualitySignal(records []Record) []QualityRow`; grouping/sort idiom mirrors `internal/scorecard/aggregate.go:122` (`Aggregate`)
- `internal/localdebt/qualitysignal_test.go` - create: co-located table-driven tests per the repo's `*_test.go` convention

## Happy Path Scenarios
**Scenario 1: Single persona/model pair with mixed statuses**
- **Given** three schema-v2 debt records for persona `security-reviewer` and model `claude-sonnet-4-6`, two with `Status: "wontfix"` and one with `Status: "resolved"`
- **When** `AggregateQualitySignal` is called with those records
- **Then** it returns exactly one `QualityRow{Persona: "security-reviewer", Model: "claude-sonnet-4-6", DismissedCount: 2, ConfirmedCount: 1}`

**Scenario 2: Multiple personas and models produce multiple rows**
- **Given** records spanning two personas (`security-reviewer`, `perf-reviewer`) and two models (`claude-sonnet-4-6`, `gpt-5.1`)
- **When** `AggregateQualitySignal` is called
- **Then** it returns one row per distinct `(persona, model)` pair actually present in the input, each with independently correct counts, and the returned slice is sorted deterministically (persona ascending, then model ascending, matching the `Aggregate()` tie-break style)

## Edge Cases
**Edge Case 1: Empty input**
- **Given** an empty `[]Record` slice
- **When** `AggregateQualitySignal` is called
- **Then** it returns an empty, non-nil `[]QualityRow` (mirrors `renderResolveJSON`'s never-null convention for downstream JSON encoding)

**Edge Case 2: Record with neither `wontfix` nor `resolved` status**
- **Given** a record with `Status: ""` (still-open) or `Status: "deferred"`
- **When** `AggregateQualitySignal` is called
- **Then** the record contributes to neither `DismissedCount` nor `ConfirmedCount` for its group, and if it is the only record for that `(persona, model)` pair, no row is emitted for that pair at all

**Edge Case 3: Repeated call is idempotent**
- **Given** the same input slice passed twice
- **When** `AggregateQualitySignal` is called both times
- **Then** both calls return byte-for-byte identical (order-stable) output — no shared mutable state leaks between calls

## Error Conditions
**Error Scenario 1: Not applicable — pure function, no error return**
- `AggregateQualitySignal` takes an in-memory `[]Record` (already read via `localdebt.ReadAll`, whose own errors are handled by the caller) and is a total function over any input, including `nil` — it has no error path of its own; malformed/short records are excluded from grouping (see AC 01-02) rather than causing a returned error.

## Performance Requirements
- **Response Time:** O(n) single pass over records for grouping, O(k log k) sort over the resulting groups (k = distinct persona/model pairs) — no nested O(n²) scans, matching `Aggregate()`'s complexity profile.
- **Throughput:** Must aggregate a store of at least 10,000 records (`.atcr/debt/` realistic multi-month scale) in well under 1 second on commodity hardware; no network or disk I/O inside the function itself (I/O is the caller's `ReadAll` responsibility).

## Security Considerations
- **Authentication/Authorization:** Not applicable — local, in-process aggregation over already-authorized local file data.
- **Input Validation:** The function must not panic on any well-formed `Record` value, including zero-value records and records with empty `Persona`/`Model` strings (Story's AC 01-02 governs exclusion rules for those); no code, file path, or problem/fix text is read or copied into `QualityRow` — only `Reviewers`, `Model`, `Status` fields are consulted.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A fixture slice of `localdebt.Record` values covering: single persona/model, multiple persona/model combinations, non-terminal statuses, and an empty slice. Hand-computed expected `QualityRow` values for each case.
**Mock/Stub Requirements:** None — pure in-memory function, no filesystem or network mocking needed for this AC (fixture records are constructed directly in the test, not read from disk).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/localdebt/...`)
- [ ] No linting errors (`go vet`, project linter)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `AggregateQualitySignal` groups by `(persona, model)` and sums dismissed/confirmed counts correctly per the fixture test
- [ ] Output ordering is deterministic across repeated calls with the same input
- [ ] Non-terminal-status records never contribute to either counter
- [ ] Empty input returns a non-nil empty slice

**Manual Review:**
- [ ] Code reviewed and approved

# Acceptance Criteria: Append-Only Record Fold by ID Before Aggregation

**Related User Story:** [01: Aggregate Per-Persona+Model Dismissed/Confirmed Counters](../user-stories/01-aggregate-per-persona-model-dismissal-counters.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go fold/dedup logic (`internal/localdebt`) | Reuses the `selectOpenDebt`-style fold pattern from `cmd/atcr/debt_resolve.go`, applied ahead of AC 01-01's grouping |
| Test Framework | Go `testing` (table-driven) | Fixture with duplicate/overwritten `ID`s across the append-only stream |
| Key Dependencies | None new | Same `Record.ID` field the resolve path already folds on |

## Related Files
- `internal/localdebt/qualitysignal.go` - modify (depends on AC 01-01): before grouping by `(persona, model)`, fold the raw `[]Record` stream by `ID` so only the terminal (resolved/wontfix) record for each id — not the original open record — contributes to the counters
- `cmd/atcr/debt_resolve.go` - reference only: `selectOpenDebt`'s fold-by-id pattern (`isClosedStatus`, `closedStatusRank`/`higherClosedStatus` for divergent-terminal-record precedence) that this AC's fold logic must mirror for the terminal (not open) side
- `internal/localdebt/qualitysignal_test.go` - modify (depends on AC 01-01): add a fixture where the same `ID` appears twice — once as the original open record (`Status: ""`), once as a later terminal record (`Status: "wontfix"`) — asserting the aggregation counts it exactly once, using the terminal record's data

### Related Files (from codebase-discovery.json)

- `internal/localdebt/qualitysignal.go` - update (AC 01-01): fold-by-`ID` pass ahead of grouping, mirroring `cmd/atcr/debt_resolve.go:156` (`selectOpenDebt`), `:129` (`closedStatusRank`), `:144` (`higherClosedStatus`)
- `internal/localdebt/qualitysignal_test.go` - update (AC 01-01): open+terminal pair, divergent-terminal, and open-only fixture streams

## Happy Path Scenarios
**Scenario 1: Open record followed by its terminal resolution counts once**
- **Given** two records sharing the same `ID`: the original open record (`Status: ""`, no `Model` yet meaningful) written by `persistLocalDebt`, and a later resolution record (`Status: "wontfix"`, `Reviewers`/`Model` carried over from the original) written by `markDebtResolved`
- **When** the aggregation runs
- **Then** exactly one dismissed count is added to the appropriate `(persona, model)` group — the open record itself never contributes a count (it has no terminal status), and the terminal record is not double-counted alongside it

**Scenario 2: Two different ids each resolve independently**
- **Given** two records with distinct `ID`s, each independently reaching a terminal status
- **When** the aggregation runs
- **Then** both contribute independently to their respective groups with no cross-contamination

## Edge Cases
**Edge Case 1: Divergent terminal records for the same id (TD-004 no-lock race)**
- **Given** the same `ID` has two terminal records appended (e.g. one `resolved`, one `wontfix`, per the documented no-lock concurrency window in `markDebtResolved`)
- **When** the aggregation folds by id
- **Then** it picks the higher-precedence terminal status deterministically (mirroring `closedStatusRank`/`higherClosedStatus`: `wontfix` outranks `resolved`) so the count is attributed once, consistently, regardless of JSONL read order

**Edge Case 2: An id with only an open record (never resolved)**
- **Given** a record whose `ID` has no terminal-status record anywhere in the stream
- **When** the aggregation folds and groups
- **Then** it contributes zero counts (neither dismissed nor confirmed) — it is still-open technical debt, not yet a quality signal

**Edge Case 3: Terminal record's own `Reviewers`/`Model` used, not the original open record's**
- **Given** an original open record and its terminal record differ in `Reviewers` or `Model` (e.g. enrichment changed between write and resolve)
- **When** the aggregation folds
- **Then** the terminal record's `Reviewers`/`Model` values are the ones used for group attribution — the fold selects the terminal record as authoritative for both status and attribution fields, consistent with `markDebtResolved` copying the original record and stamping status atop it

## Error Conditions
**Error Scenario 1: Not applicable**
- Folding is a pure in-memory pass with no error return; a stream with only open records (no terminal ones) is a valid, common case (a working backlog), not an error.

## Performance Requirements
- **Response Time:** Fold-by-id is O(n) using a map keyed by `ID`, matching `selectOpenDebt`'s existing complexity — no quadratic id-matching scan.
- **Throughput:** Combined with AC 01-01's grouping pass, the full pipeline (fold + group + sort) remains O(n + k log k) end-to-end.

## Security Considerations
- **Authentication/Authorization:** Not applicable.
- **Input Validation:** No new external input; reads only `ID`, `Status`, `Reviewers`, `Model` already validated by the read path.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture streams with: (a) open + terminal pair sharing an id, (b) two divergent terminal records sharing an id, (c) an id with only an open record, (d) multiple distinct ids each independently terminal.
**Mock/Stub Requirements:** None — pure in-memory fold over constructed `[]Record` fixtures, no filesystem needed for this AC's unit tests.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/localdebt/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] An open+terminal pair sharing one `ID` counts exactly once, using the terminal record
- [ ] Divergent terminal records for one id resolve to the higher-precedence status deterministically
- [ ] An id with no terminal record contributes zero counts
- [ ] Fold-by-id complexity stays O(n) (no per-id linear scan of the whole stream)

**Manual Review:**
- [ ] Code reviewed and approved

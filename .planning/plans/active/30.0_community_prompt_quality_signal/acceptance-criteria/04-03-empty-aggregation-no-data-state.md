# Acceptance Criteria: Empty-Aggregation "No Data" State

**Related User Story:** [04: Maintainer-Facing Prompt Quality Report](../user-stories/04-maintainer-facing-prompt-quality-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go Cobra subcommand render branch | `cmd/atcr/telemetry_report.go` — a guard clause before the ranked-table render path |
| Test Framework | Go `testing` (table-driven, plus a CLI exit-code assertion) | Mirrors `cmd/atcr/report.go`'s absent-data usage-error convention |
| Key Dependencies | `internal/localdebt.AggregateQualitySignal` returning an empty, non-nil `[]QualityRow` (Story 1 Edge Case 1) | No new dependency |

## Related Files
- `cmd/atcr/telemetry_report.go` - create: `runQualityReport` checks `len(rows) == 0` and renders a clear "no data" message (both `md` and `json` formats) instead of an empty table
- `cmd/atcr/telemetry_report_test.go` - create: test asserting an empty `[]QualityRow` input produces the "no data" message and a clean (zero) process exit, not a panic or an ambiguous empty table
- `internal/localdebt/qualitysignal.go` - reference only: `AggregateQualitySignal` returns an empty, non-nil slice for empty/no-opt-in input (Story 1 Edge Case 1) — this AC's guard clause consumes that contract directly, no additional nil-check needed
- `cmd/atcr/report.go` - reference only: contrast case — `runReport`'s absent-`findings.json` path returns a usage error (exit 2); this AC deliberately renders exit 0 instead, since "no quality-signal data yet" is an expected steady state for an opt-in feature, not a misconfiguration

## Happy Path Scenarios
**Scenario 1: No opt-in data collected yet renders a clear message in markdown**
- **Given** `AggregateQualitySignal` returns an empty `[]QualityRow` (quality-signal collection has never been enabled on this machine, per Epic AC1)
- **When** the command runs with `--format md` (default)
- **Then** it prints a clear, human-readable "no data" message (e.g. explaining that quality-signal collection is opt-in and no data has been aggregated yet) and exits 0

**Scenario 2: No data renders a well-formed empty payload in JSON format**
- **Given** the same empty aggregation as Scenario 1
- **When** the command runs with `--format json`
- **Then** it renders a well-formed JSON value representing "no data" (e.g. an empty array `[]`, not `null`, and not a malformed/partial JSON document) and exits 0

## Edge Cases
**Edge Case 1: Quality-signal collection enabled but zero records collected so far**
- **Given** the opt-in gate (Story 2) is enabled but no dismissed/confirmed events have occurred yet, so `AggregateQualitySignal` still returns an empty slice
- **When** the report runs
- **Then** it renders the same "no data" state as Scenario 1 — the command does not attempt to distinguish "opt-in never enabled" from "opt-in enabled but zero events" (both produce an empty aggregation and an identical, non-misleading message)

**Edge Case 2: Aggregation becomes non-empty on a subsequent run**
- **Given** a first run with empty aggregation followed by a second run after data exists
- **When** the report runs the second time
- **Then** it renders the full ranked table (AC 04-01), confirming the "no data" branch is a genuine conditional guard and not a cached/sticky state

## Error Conditions
**Error Scenario 1: Not a panic**
- Error message: not applicable — this is precisely the failure mode being prevented; the test asserts the process does NOT panic and does NOT return a non-zero unexpected exit code for a legitimately empty aggregation.
- HTTP status / error code: process exit code 0 (success — this is expected steady state, not an error)

**Error Scenario 2: Distinguish from a genuine read failure**
- **Given** `localdebt.ReadAll` itself returns an error (corrupt store), as opposed to a successful read yielding zero rows
- **When** the command runs
- **Then** it takes the AC 04-01 Error Scenario 2 path (exit 1, wrapped error) — not the "no data" exit-0 path — so a real failure is never silently presented as "no data yet"

## Performance Requirements
- **Response Time:** The empty-check adds negligible overhead (a single length check) to the existing render path's performance budget (AC 04-01).
- **Throughput:** Not applicable — single-shot CLI invocation.

## Security Considerations
- **Authentication/Authorization:** Not applicable — local, read-only CLI command.
- **Input Validation:** No user input is involved in reaching this branch; the guard is driven entirely by the aggregation's row count, a value the command already trusts from Story 1's contract.

## Test Implementation Guidance
**Test Type:** UNIT + one CLI-level (E2E-lite) exit-code assertion
**Test Data Requirements:** An empty (but non-nil) `[]localdebt.QualityRow` fixture; a corrupt-store fixture to distinguish Error Scenario 2 from the happy-path empty case.
**Mock/Stub Requirements:** Stub `AggregateQualitySignal`'s output (empty slice) and separately stub `ReadAll` returning an error, to test the two branches independently.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors (`go vet`, project linter)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Empty aggregation renders a clear "no data" message in both `md` and `json` formats and exits 0
- [ ] The command never panics on an empty (but non-nil) aggregation input
- [ ] A genuine read failure (corrupt store) is never conflated with the "no data" exit-0 path — it exits 1 per AC 04-01 Error Scenario 2
- [ ] A subsequent run with real data renders the full ranked table, proving the guard is a live conditional, not a cached state

**Manual Review:**
- [ ] Code reviewed and approved

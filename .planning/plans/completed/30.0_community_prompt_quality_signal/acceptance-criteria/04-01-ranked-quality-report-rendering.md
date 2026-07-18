# Acceptance Criteria: Ranked Per-Persona+Model Quality Report Rendering

**Related User Story:** [04: Maintainer-Facing Prompt Quality Report](../user-stories/04-maintainer-facing-prompt-quality-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go Cobra subcommand (`cmd/atcr` package) | New file `cmd/atcr/telemetry_report.go`, mirrors `newReportCmd`/`runReport` in `cmd/atcr/report.go` |
| Test Framework | Go `testing` (table-driven) | Mirrors `internal/report/render_test.go` conventions |
| Key Dependencies | `internal/localdebt.AggregateQualitySignal` (Story 1), `spf13/cobra`, `sort` (stdlib) | No new third-party dependency |

## Related Files
- `cmd/atcr/telemetry_report.go` - create: `newQualityReportCmd()`/`runQualityReport()` — Cobra command that calls `localdebt.ReadAll` + `localdebt.AggregateQualitySignal`, ranks the returned `[]QualityRow` by dismissal rate, and renders `--format md|json`
- `cmd/atcr/telemetry_report_test.go` - create: table-driven test asserting rendered ranking order and displayed counters exactly match a fixture aggregation's hand-computed expected values
- `internal/localdebt/qualitysignal.go` - reference only: `AggregateQualitySignal(records []Record) []QualityRow` (Story 1) is the sole data source this command renders
- `cmd/atcr/main.go` - modify: register the new subcommand in `root.AddCommand(...)` alongside `newReportCmd()`

### Related Files (from codebase-discovery.json)

- `cmd/atcr/telemetry_report.go` - create: `newQualityReportCmd()`/`runQualityReport()`, mirroring `cmd/atcr/report.go:18` (`newReportCmd`) and `:31` (`runReport`)
- `cmd/atcr/telemetry_report_test.go` - create: table-driven render tests
- `cmd/atcr/main.go` - update: register the subcommand in `root.AddCommand(...)` (`:212-217`)

## Happy Path Scenarios
**Scenario 1: Fixture aggregation renders correctly ranked markdown table**
- **Given** a fixture `[]QualityRow` with three persona+model pairs with known `DismissedCount`/`ConfirmedCount` values (dismissal rate = `Dismissed / (Dismissed + Confirmed)`)
- **When** the command runs with `--format md` (the default)
- **Then** the output is a markdown table listing persona, model, dismissed count, confirmed count, and dismissal rate, with rows sorted by dismissal rate descending, tie-broken by persona then model ascending (matching `Aggregate()`'s tie-break style)

**Scenario 2: JSON format renders the same ranked data as a structured payload**
- **Given** the same fixture aggregation as Scenario 1
- **When** the command runs with `--format json`
- **Then** the output is a JSON array of objects (one per persona+model pair) in the identical rank order as the markdown rendering, with fields limited to persona, model, dismissed count, confirmed count, and dismissal rate

**Scenario 3: Highest-dismissal and lowest-confirmation rows are visually distinguishable**
- **Given** a fixture with one persona+model pair at 90% dismissal rate and another at 5%
- **When** the report is rendered in either format
- **Then** the 90%-dismissal row appears first (over-reporting candidate) and the report's heading/column labeling makes clear that descending dismissal rate is the ranking basis (no unexplained ordering)

## Edge Cases
**Edge Case 1: Single persona+model pair**
- **Given** an aggregation with exactly one `QualityRow`
- **When** the report renders
- **Then** it renders that one row without error, with a dismissal rate computed from its own counts (no divide-by-zero when `DismissedCount + ConfirmedCount == 0`, which defers to AC 04-03's "no data" handling only when the row set itself is empty)

**Edge Case 2: Tied dismissal rates across multiple pairs**
- **Given** two or more `QualityRow` entries with identical dismissal rates
- **When** the report renders
- **Then** the tie is broken deterministically by persona ascending, then model ascending — repeated runs over the same input produce byte-for-byte identical output

## Error Conditions
**Error Scenario 1: Unsupported `--format` value**
- **Given** `--format xml` (not `md` or `json`)
- **When** the command runs
- **Then** it returns a usage error before any aggregation read: "unknown format \"xml\": supported formats are md, json"
- HTTP status / error code: process exit code 2 (usage error, matching `usageError` in `cmd/atcr/main.go`)

**Error Scenario 2: Underlying debt store is unreadable/corrupt**
- **Given** `localdebt.ReadAll` returns an error (e.g. malformed on-disk record)
- **When** the command runs
- **Then** it returns a wrapped error (not a panic) and exits non-zero (exit 1, matching the `codedError{code: exitFailure}` convention in `cmd/atcr/report.go`)

## Performance Requirements
- **Response Time:** Rendering completes in well under 1 second for a realistic multi-month `.atcr/debt/` store (up to ~10,000 records), consistent with `AggregateQualitySignal`'s O(n) + O(k log k) complexity budget (Story 1 AC 01-01).
- **Throughput:** Single-shot CLI invocation; no concurrent-request handling required.

## Security Considerations
- **Authentication/Authorization:** Not applicable — local, read-only CLI command over already-authorized local file data; no network calls.
- **Input Validation:** `--format` is validated against an explicit enum before any I/O (mirrors `report.ValidFormat` in `cmd/atcr/report.go`); no user-supplied path/content is interpolated into rendered output beyond persona/model identifier strings already present in `QualityRow`.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixture `[]localdebt.QualityRow` slices covering: multiple distinct pairs with varying dismissal rates, a single pair, and tied rates. Hand-computed expected markdown and JSON output strings/structures.
**Mock/Stub Requirements:** Stub or in-memory `AggregateQualityRow` input (bypass `localdebt.ReadAll` disk I/O in unit tests by injecting a fixture slice directly into the render function, matching `internal/report/render_test.go`'s pattern of testing the render function separately from the CLI I/O wrapper).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/... ./internal/localdebt/...`)
- [ ] No linting errors (`go vet`, project linter)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Rendered ranking order matches hand-computed dismissal-rate-descending order in a table-driven test
- [ ] `--format md` and `--format json` both render the identical row set and rank order
- [ ] Tied dismissal rates resolve deterministically (persona then model ascending)
- [ ] Unsupported `--format` value fails as a usage error (exit 2) before any data read

**Manual Review:**
- [ ] Code reviewed and approved

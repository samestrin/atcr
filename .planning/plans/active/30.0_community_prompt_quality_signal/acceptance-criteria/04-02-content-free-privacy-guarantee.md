# Acceptance Criteria: Content-Free Privacy Guarantee on the Report Render Path

**Related User Story:** [04: Maintainer-Facing Prompt Quality Report](../user-stories/04-maintainer-facing-prompt-quality-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go Cobra subcommand + static import guard | `cmd/atcr/telemetry_report.go` render path; enforced structurally by the `QualityRow` type shape, verified by an import-graph test |
| Test Framework | Go `testing` + `go/build` (or `go list`) import inspection | New test asserts the report's source file has no import of `internal/reconcile` or raw findings types |
| Key Dependencies | `internal/localdebt.QualityRow` (Story 1) — the only struct the render path may consume | No new third-party dependency |

## Related Files
- `cmd/atcr/telemetry_report.go` - create: render path sources exclusively from `[]localdebt.QualityRow{Persona, Model, DismissedCount, ConfirmedCount}` — never from `reconcile.JSONFinding` or `findings.json`
- `cmd/atcr/telemetry_report_import_test.go` - create: static test asserting `cmd/atcr/telemetry_report.go` has no import of `github.com/samestrin/atcr/internal/reconcile` and does not call `readReconciledFindings` (the helper `cmd/atcr/report.go` uses)
- `cmd/atcr/report.go` - reference only: the existing findings-rendering path this story's command must NOT reuse or alias
- `internal/localdebt/qualitysignal.go` - reference only: `QualityRow`'s field set (Persona, Model, DismissedCount, ConfirmedCount) is the type-level ceiling on what the report can ever display

## Happy Path Scenarios
**Scenario 1: Rendered output contains only allowlisted fields**
- **Given** a fixture aggregation with realistic persona names, model identifiers, and counters
- **When** the report renders in `md` or `json` format
- **Then** the output contains only persona identifier, model identifier, dismissed count, confirmed count, and derived dismissal rate — no other field is present in either rendering

**Scenario 2: Static import check confirms no path to raw findings data**
- **Given** the compiled `cmd/atcr/telemetry_report.go` source
- **When** the import-graph test inspects its import list (e.g. via `go/parser` or `go list -deps`)
- **Then** it asserts `internal/reconcile` and `internal/report`'s findings-rendering entry points are absent from the file's direct imports and call graph

## Edge Cases
**Edge Case 1: Persona or model identifier string happens to resemble file-path-like content**
- **Given** a persona or model name containing path-like characters (e.g. a hypothetical `model: "gpt-5/preview"`)
- **When** the report renders
- **Then** the string is displayed verbatim as an identifier field (not interpreted as, or conflated with, a file path) — the render path performs no path resolution or file content lookup keyed by these strings

**Edge Case 2: Adjacent code accidentally imports `internal/reconcile` transitively**
- **Given** a future change adds a helper function to `cmd/atcr/telemetry_report.go` that imports `internal/reconcile` for an unrelated reason (e.g. shared error type)
- **When** the import-graph test runs
- **Then** it fails loudly, flagging the violation before merge (regression guard, not just an initial-implementation check)

## Error Conditions
**Error Scenario 1: Not applicable — this AC is a structural/type-level guarantee, not a runtime error path**
- There is no runtime error scenario specific to content exposure: the guarantee is enforced by `QualityRow`'s field set (no `Content`, `FilePath`, or `Code` field exists to leak) plus the static import test, not by a runtime filter that could be bypassed.

## Performance Requirements
- **Response Time:** Not applicable — this AC adds no runtime cost; the import-graph test runs as part of the normal `go test` suite (sub-second).
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — no new access surface; this AC narrows what the existing local CLI can display.
- **Input Validation:** Enforced at the type level: the render function's parameter type is `[]localdebt.QualityRow`, which structurally cannot carry finding text, file paths, or code — there is no field to validate away because no such field exists on the input type.

## Test Implementation Guidance
**Test Type:** UNIT (static analysis style)
**Test Data Requirements:** The source file `cmd/atcr/telemetry_report.go` itself (inspected via `go/parser`/`go list`, not executed) plus a fixture `QualityRow` slice for the rendered-fields assertion.
**Mock/Stub Requirements:** None — import inspection is static; rendered-fields assertion uses an in-memory fixture, no filesystem or network mocking.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors (`go vet`, project linter)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Rendered output (md and json) contains only persona, model, dismissed count, confirmed count, and dismissal rate — no other field
- [ ] Static import test confirms `cmd/atcr/telemetry_report.go` has no import of `internal/reconcile` or the findings-rendering path
- [ ] The guard test is a regression check that would fail on a future accidental import, not a one-time manual verification

**Manual Review:**
- [ ] Code reviewed and approved

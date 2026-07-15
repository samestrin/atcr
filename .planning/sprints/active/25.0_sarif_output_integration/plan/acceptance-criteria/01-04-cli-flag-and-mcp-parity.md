# Acceptance Criteria: CLI Flag Help Text and MCP Parity

**Related User Story:** [01: SARIF Formatter Core](../user-stories/01-sarif-formatter-core.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go CLI (Cobra flag definition, `cmd/atcr/report.go`) + Go package regression test (`internal/mcp`) | `--format` flag already generalizes over `report.ValidFormat`/`report.Render`; only its help text and test coverage change |
| Test Framework | `go test` (CLI-path integration test + MCP handler regression test) | New/extended tests in `cmd/atcr` (or existing report-command test file) and `internal/mcp` |
| Key Dependencies | `github.com/spf13/cobra` (already used) | No new dependency |

### Related Files (from codebase-discovery.json)

- [`cmd/atcr/report.go`](../../../../../cmd/atcr/report.go) — modify:
  - Update the `--format` flag help text from `"output format: md, json, or checklist"` to include `sarif` (e.g. `"output format: md, json, checklist, or sarif"`) ([`cmd/atcr/report.go:25`](../../../../../cmd/atcr/report.go)).
  - Update the command's `Short` description (`"Render md, json, or checklist views over reconciled findings"`) to include `sarif` ([`cmd/atcr/report.go:21`](../../../../../cmd/atcr/report.go)) — it enumerates the same format list and also appears in `atcr report --help`, so leaving it stale omits `sarif` from the help summary line.
  - The existing `runReport` flow ([`cmd/atcr/report.go:31-128`](../../../../../cmd/atcr/report.go)) already validates the format via `report.ValidFormat()` and routes to `report.Render()`; no new command wiring is required.
  - The existing `--disagreements` incompatibility guard ([`cmd/atcr/report.go:97`](../../../../../cmd/atcr/report.go)) automatically rejects `--format sarif`.
- [`internal/mcp/handlers.go`](../../../../../internal/mcp/handlers.go) — reference only (no code change expected): `handleReport` ([`internal/mcp/handlers.go:370-420`](../../../../../internal/mcp/handlers.go)) validates the format through `report.ValidFormat()` and routes non-markdown output through `report.Render()`, so SARIF becomes available to MCP clients automatically.
- [`cmd/atcr/report_test.go`](../../../../../cmd/atcr/report_test.go) (or equivalent existing CLI test file) — modify: add a test invoking `atcr report --format=sarif` (or `runReport` directly) against a fixture reconciled directory, asserting the CLI output matches calling `report.Render(&buf, findings, report.FormatSarif)` directly byte-for-byte.
- [`internal/mcp/handlers_test.go`](../../../../../internal/mcp/handlers_test.go) (or equivalent existing MCP handler test file) — modify: add a regression test calling `handleReport` with `format: "sarif"` and asserting it returns the same output `report.Render` would produce directly.
- [`internal/report/render.go`](../../../../../internal/report/render.go) — reference: `FormatSarif`, `ValidFormat()`, `Formats()`, and `Render()` must already recognize SARIF (AC 01-01).

### Technical References

- [GitHub Code Scanning SARIF Integration Constraints](../documentation/github-code-scanning-integration.md)
- [SARIF 2.1.0 Schema Reference](../documentation/sarif-schema-reference.md)

## Happy Path Scenarios
**Scenario 1: --format=sarif is accepted at the CLI and produces SARIF output**
- **Given** a reconciled review directory with `findings.json` present
- **When** `atcr report --format=sarif <id-or-path>` is run
- **Then** the command exits 0 and stdout contains a valid SARIF 2.1.0 JSON document (same shape as AC 01-02/01-03 assert directly against `renderSarif`)

**Scenario 2: CLI output matches direct report.Render call**
- **Given** the same findings fixture used for the CLI test
- **When** the output of `atcr report --format=sarif` is compared to calling `report.Render(&buf, findings, report.FormatSarif)` directly in a Go test
- **Then** the two outputs are byte-identical — the CLI layer adds no formatting divergence

**Scenario 3: MCP handleReport produces SARIF parity with the CLI**
- **Given** the same findings fixture
- **When** `handleReport` (`internal/mcp/handlers.go`) is invoked with `format: "sarif"`
- **Then** it returns output identical to `report.Render(&buf, findings, report.FormatSarif)` — confirming SARIF became available to MCP clients automatically via the shared `Render()` call, with no MCP-specific code required

## Edge Cases
**Edge Case 1: help text lists sarif alongside the other three formats**
- **Given** `atcr report --help`
- **When** the help output is inspected
- **Then** the `--format` flag's description string contains `sarif` (not just the pre-existing `md, json, or checklist`)

**Edge Case 2: --format=bogus still surfaces sarif in the CLI-level error**
- **Given** `atcr report --format=bogus <id-or-path>`
- **When** the command is run
- **Then** it exits with a usage error (exit code 2, per the existing `usageError` convention at `cmd/atcr/report.go:35-37`) whose message enumerates `sarif` among supported formats — this is the CLI-level manifestation of AC 01-01 Edge Case 2

**Edge Case 3: --format=sarif combined with --output writes the SARIF document to a file**
- **Given** `atcr report --format=sarif --output out.sarif.json <id-or-path>`
- **When** the command completes
- **Then** `out.sarif.json` contains the same bytes that would have gone to stdout — the existing `--output` plumbing (`cmd/atcr/report.go:44-50`) requires no SARIF-specific changes, confirmed by this test

## Error Conditions
**Error Scenario 1: --disagreements is incompatible with --format=sarif (existing constraint, verify it still applies)**
- **Given** `atcr report --disagreements --format=sarif <id-or-path>` (mirroring the existing `--disagreements` + non-markdown-format incompatibility already enforced for `checklist`/`json` at `cmd/atcr/report.go:97`)
- **When** the command is run
- Error message: `"--disagreements does not support --format sarif"` (same message template as the existing checklist/json case, with `sarif` substituted)
- Exit code: 2 (usage error), no output written

**Error Scenario 2: missing reconciled findings.json**
- **Given** an id-or-path with no reconciled `findings.json` present
- **When** `atcr report --format=sarif <id-or-path>` is run
- Error message: the existing "run reconcile first" guidance already produced for the md/json/checklist formats today (no SARIF-specific error path — this AC confirms the existing pre-render guard fires identically regardless of `--format` value)
- Exit code: matches the existing non-sarif behavior (this AC does not change error handling upstream of `Render()`)

## Performance Requirements
- **Response Time:** CLI invocation overhead is unchanged from the existing three formats — `--format` parsing and validation is O(1); no new I/O is introduced by adding `sarif` as an accepted value.
- **Throughput:** N/A (single CLI invocation per run).

## Security Considerations
- **Authentication/Authorization:** N/A — local CLI process, same trust boundary as the existing `md`/`json`/`checklist` formats; `--format=sarif` does not open a new access path to the reconciled findings.
- **Input Validation:** `--format` remains validated against the closed `ValidFormat` enum before any I/O (per the existing TD-003 comment at `cmd/atcr/report.go:32-34`); `--output` path resolution/validation (`resolveOutputPath`, lines 44-50) is unchanged and applies identically regardless of `--format` value, so `--format=sarif --output <path>` cannot bypass the existing symlink/system-directory guard.

## Test Implementation Guidance
**Test Type:** INTEGRATION (CLI command execution) + UNIT (MCP handler regression)
**Test Data Requirements:** A fixture reconciled directory with a `findings.json` (reuse or mirror the `sample()` fixture's two findings) set up via a temp dir in the test, matching the existing CLI test pattern for `md`/`json`/`checklist`.
**Mock/Stub Requirements:** None beyond the existing CLI test harness's temp-directory setup already used for the other formats; the MCP regression test may reuse whatever request/response test doubles `internal/mcp/handlers_test.go` already defines for `handleReport`.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./...`)
- [x] No linting errors
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `atcr report --format=sarif` produces the same output as `report.Render(..., FormatSarif)` (CLI/library parity test)
- [x] `--help` text for `--format` mentions `sarif`
- [x] The `report` command's `Short` description (`cmd/atcr/report.go:21`) also lists `sarif`, so the `--help` summary line is not stale
- [x] `--format=bogus` error message lists `sarif`
- [x] `handleReport` with `format: "sarif"` returns output identical to direct `report.Render` (MCP parity regression test)

**Manual Review:**
- [x] Code reviewed and approved

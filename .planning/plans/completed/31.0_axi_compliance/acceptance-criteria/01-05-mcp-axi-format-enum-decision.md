# Acceptance Criteria: MCP Format-Enum Propagation Decision for `FormatAXI`

**Related User Story:** [01: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`](../user-stories/01-axi-token-dense-output-mode.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go MCP server (`internal/mcp` package), JSON Schema enum generation | `report.FormatList()` is the single source of truth both the CLI help text and the MCP schema enum derive from |
| Test Framework | `go test` with `testify/assert`; JSON Schema validation assertions | Mirrors existing `reportInputSchema` tests |
| Key Dependencies | `github.com/google/jsonschema-go` (or equivalent) via `jsonschema.For[ReportArgs]` | No new dependency |

### Related Files (from codebase-discovery.json)
- `internal/mcp/tools.go` - modify (or explicitly leave unmodified with a documented decision): `descReport` (line 216-218) and `reportInputSchema` (line 224-241) derive the format enum from `report.FormatList()`/`report.Formats()` — if `FormatAXI` is added to `internal/report`'s enum (AC 01-01), it propagates here automatically unless explicitly excluded with a filtered enum list.
- `internal/mcp/handlers.go` - modify (or confirm unmodified): `handleReport` (line ~370-380) validates `format` via `report.ValidFormat(format)` as defense-in-depth behind the JSON Schema enum; if `FormatAXI` propagates, an MCP client requesting `format: "axi"` must be handled the same way any other format is (findings render to the `content` field of the MCP tool result).
- `internal/report/render.go` - reference: the `FormatList()`/`Formats()` functions (line 44-53) that both the CLI and MCP consume — this AC does not modify them, only decides/verifies how their output is consumed downstream.
- `.planning/plans/active/31.0_axi_compliance/documentation/mcp-schema-format-propagation.md` - reference only: documents the `report.FormatList()` → MCP schema enum/description propagation mechanics and the sprint 25.0 (SARIF) CLI/MCP-parity precedent (`.planning/plans/completed/25.0_sarif_output_integration`, AC 01-04) that this include/exclude decision follows.

## Happy Path Scenarios
**Scenario 1: `FormatAXI` propagates to the MCP schema enum (if the design decision is "include")**
- **Given** `FormatAXI` has been added to `report.FormatList()` (AC 01-01)
- **When** an MCP client calls `atcr_report` with `format: "axi"`
- **Then** `reportInputSchema`'s JSON Schema enum (tools.go line 233-238) accepts `"axi"` as a valid value, `handleReport`'s defense-in-depth check (`report.ValidFormat("axi")`) passes, and the tool result content contains the same axi-encoded payload `atcr report --axi` would produce on stdout

**Scenario 2: `FormatAXI` is explicitly excluded from MCP (if the design decision is "exclude")**
- **Given** the sprint design decides `--axi` is a CLI-subprocess-only feature not meant for MCP tool consumption
- **When** `reportInputSchema` builds its enum
- **Then** the enum is built from a filtered list (e.g. `report.FormatList()` minus `FormatAXI`) rather than the raw `FormatList()`, and this exclusion is documented inline with a comment explaining why (mirroring the story's Constraint: "`--axi` propagating into MCP's `atcr_report` format enum via `report.FormatList()` is acceptable unless explicitly excluded")

## Edge Cases
**Edge Case 1: An MCP client sends `format: "axi"` when it has been explicitly excluded**
- **Given** the "exclude" decision from Scenario 2
- **When** an MCP client requests `format: "axi"` anyway
- **Then** the request is rejected by the JSON Schema enum validation before the handler runs (the enum simply does not list `"axi"` as an allowed value), and `handleReport`'s defense-in-depth `report.ValidFormat` check would also reject it if reached — same double-layer defense pattern the existing formats use

**Edge Case 2: MCP tool description text**
- **Given** `descReport` (tools.go line 216-218) is built from `report.Formats()`, a comma-joined string
- **When** `FormatAXI` is included in `FormatList()`
- **Then** `descReport`'s optional-args description text automatically lists `axi` alongside `md, json, checklist, sarif` with no manual doc-string edit required — the var-not-const pattern (tools.go line 214-215 comment) is exercised as designed

## Error Conditions
**Error Scenario 1: Invalid format still rejected identically**
- **Given** an MCP client requests an unsupported format string (e.g. `"toon"` instead of `"axi"`)
- **When** the request reaches the `atcr_report` tool
- **Then** the existing rejection behavior (JSON Schema enum validation, then `handleReport`'s `report.ValidFormat` backstop) is unchanged by this AC's work
- Error message: `"invalid format: %s; must be one of: %s"` (handlers.go, unchanged format string)

## Performance Requirements
- **Response Time:** No measurable change — enum construction is a small, in-memory slice build at server-startup/schema-generation time, not a per-request cost.
- **Throughput:** N/A.

## Security Considerations
- **Authentication/Authorization:** None new — MCP server auth/exposure is unchanged; `descMetrics`'s existing "local-only" note (tools.go line 210-211) sets the precedent that MCP tool descriptions call out exposure constraints when relevant, applicable if `--axi` payload size/pagination (Story 3/4 territory) turns out to matter for MCP clients.
- **Input Validation:** Unchanged — the `format` argument continues to be validated by both the JSON Schema enum and `report.ValidFormat` as defense-in-depth; no new unvalidated input path is introduced.

## Test Implementation Guidance
**Test Type:** UNIT (schema-enum construction test) + INTEGRATION (MCP `handleReport` call with `format: "axi"`, exercising whichever decision — inclusion or exclusion — the sprint design makes)
**Test Data Requirements:** A minimal `ReportArgs{Format: "axi"}` fixture and a fixture reconciled findings directory.
**Mock/Stub Requirements:** None beyond what existing `internal/mcp` handler tests already use (in-process MCP server/handler invocation, no live transport needed).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/mcp/...`)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A single explicit decision (include vs. exclude `FormatAXI` from the MCP enum) is made and documented in code comments, not left as an accidental side effect of AC 01-01's enum change
- [ ] `reportInputSchema`'s generated JSON Schema enum matches the decision (contains or omits `"axi"`)
- [ ] `descReport`'s generated description text matches the decision
- [ ] `handleReport`'s defense-in-depth validation behavior is verified consistent with the decision (accepts or rejects `format: "axi"` as intended)

**Manual Review:**
- [ ] Code reviewed and approved

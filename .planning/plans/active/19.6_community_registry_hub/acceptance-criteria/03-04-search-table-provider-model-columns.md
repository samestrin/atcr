# Acceptance Criteria: `renderPersonaSearch` Table Displays Provider/Model Columns

**Related User Story:** [03: Model-Aware Search and Discovery via `--model`/`--provider`](../user-stories/03-model-aware-search-and-discovery.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go rendering function (`cmd/atcr/personas.go`, `renderPersonaSearch`) | Uses existing `writeTable`/`tabwriter` helper (line ~375) |
| Test Framework | Go `testing` package, output-buffer comparison (`bytes.Buffer` + `cmd.SetOut`) | Matches existing render-function test style (`renderPersonaList`, `renderScoredList`) |
| Key Dependencies | stdlib `text/tabwriter`, `fmt`, `io` | No new third-party dependency |

## Related Files
- `cmd/atcr/personas.go` - modify: `renderPersonaSearch` (line ~417) — add `PROVIDER`/`MODEL` columns to the header and each row, alongside the existing `NAME`/`VERSION`/`DESCRIPTION` columns, using the same `"-"` placeholder convention as the existing empty-`Version` handling
- `cmd/atcr/personas_test.go` - create/modify: test asserting the rendered table header and rows include `PROVIDER`/`MODEL` columns with correct values, including the `"-"` placeholder case for entries with empty `Provider`/`Model`

## Happy Path Scenarios
**Scenario 1: Search results render Provider and Model columns**
- **Given** search results containing a persona with `Name: "deepseek-reviewer", Version: "1.0.0", Provider: "deepseek", Model: "deepseek-coder", Description: "Coder-tuned"`
- **When** `renderPersonaSearch` is called with this entry
- **Then** the rendered table includes a header row with `PROVIDER` and `MODEL` columns and a data row showing `deepseek` and `deepseek-coder` in those columns, so a user can visually confirm the target model before installing

**Scenario 2: End-to-end CLI output shows Provider/Model for `--model` search results**
- **Given** a mock registry with a persona matching `--model deepseek`
- **When** `atcr personas search --model deepseek` is run and stdout is captured
- **Then** the printed table includes the Provider/Model columns populated for the matching persona

## Edge Cases
**Edge Case 1: Empty Provider or Model renders the existing placeholder convention**
- **Given** a persona entry with `Provider: ""` and `Model: ""` (e.g. a general-purpose, non-model-specific persona)
- **When** `renderPersonaSearch` renders this entry
- **Then** both the `PROVIDER` and `MODEL` columns show `"-"`, matching the existing placeholder convention already used for empty `Version` (line ~421-423)

**Edge Case 2: Column order/count does not break existing table alignment**
- **Given** the `writeTable`/`tabwriter` helper (line ~375) used by all persona list/search renderers
- **When** the 5-column header (`NAME\tVERSION\tPROVIDER\tMODEL\tDESCRIPTION` or similar ordering) is passed
- **Then** `tabwriter` aligns all columns correctly regardless of value width, with no panic or misalignment for empty-string cells

## Error Conditions
**Error Scenario 1: Rendering an empty result set**
- **Given** zero entries passed to `renderPersonaSearch`
- **When** the function is called
- **Then** only the header row is printed (no data rows), matching existing behavior for zero-length `rows` in `writeTable` — this path is already reached via the "No personas found" short-circuit in `RunE` before `renderPersonaSearch` is even called, so this is a defensive/regression check on the render function in isolation
- HTTP status / error code: N/A (presentation-only function, no error return expected in the empty case)

## Performance Requirements
- **Response Time:** Table rendering remains O(n) over result count with negligible per-row formatting cost; no measurable regression from adding two columns.
- **Throughput:** N/A (single-user CLI invocation).

## Security Considerations
- **Authentication/Authorization:** N/A — presentation-only function over already-validated struct fields.
- **Input Validation:** `Provider`/`Model` values are rendered as-is (already JSON-decoded strings from the fetched index); no additional escaping needed since output is a plain-text terminal table, consistent with existing column rendering (`Name`, `Version`, `Description`).

## Test Implementation Guidance
**Test Type:** UNIT (direct `renderPersonaSearch` calls against a `bytes.Buffer`)
**Test Data Requirements:** A small slice of `PersonaIndexEntry` values covering: populated Provider/Model, empty Provider/Model, and a mix within the same call
**Mock/Stub Requirements:** None — `renderPersonaSearch` takes an `io.Writer` and a slice directly, no network or filesystem dependency

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `renderPersonaSearch` header includes `PROVIDER` and `MODEL` columns
- [ ] Populated `Provider`/`Model` values render correctly in their columns
- [ ] Empty `Provider`/`Model` values render as `"-"` per existing placeholder convention
- [ ] Table alignment via `tabwriter` is unaffected by the new columns

**Manual Review:**
- [ ] Code reviewed and approved

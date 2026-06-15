# Acceptance Criteria: Scorecard Table Rendering and Conditional Columns

**Related User Story:** [02: View Single-Run Scorecard](../user-stories/02-view-single-run-scorecard.md)

## Acceptance Criteria Statement
The `atcr scorecard` command renders per-reviewer records as an aligned table using `text/tabwriter`, with verification columns shown only when the records contain verification data.

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Table Renderer | `text/tabwriter` (Go stdlib) | Matches `internal/doctor/render.go` pattern |
| Column Formatting | `fmt.Fprintf` with format string | `"%s\t%s\t%d\t%d\t%d\t%.2f\t$%.4f\t%dms"` |
| Conditional Columns | Runtime column detection | Verification columns added only when data present |
| Test Framework | `go test` + `testify/assert` | Golden-file comparison for table output |
| Key Dependencies | `bytes.Buffer` for test capture | Capture `cmd.OutOrStdout()` output |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/scorecard.go` — modify: render logic with `text/tabwriter`, column selection
- `internal/scorecard/store.go` — reference: `Record` struct fields define available columns
- `internal/doctor/render.go:45` — reference: `RenderTable` tabwriter usage pattern
- `cmd/atcr/scorecard_test.go` — create: golden-file tests for table output

## Happy Path Scenarios

**Scenario 1: Standard table without verification data**
- **Given** 3 scorecard records for reviewers Alice (gemini-2.5-pro), Bob (claude-sonnet-4-20250514), Carol (gpt-4o) with fields: `findings_raised`, `findings_corroborated`, `findings_solo`, `corroboration_rate`, `cost_usd`, `latency_ms`
- **And** none of the records have `findings_verified` set
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the command renders a table with columns: `REVIEWER`, `MODEL`, `RAISED`, `CORROBORATED`, `SOLO`, `CORR%`, `COST`, `LATENCY`
- **And** the table header is followed by 3 data rows, one per reviewer
- **And** corroboration rate displayed as percentage (e.g., `58%`)
- **And** cost displayed with 4 decimal places (e.g., `$0.0234`)
- **And** latency displayed in milliseconds (e.g., `3400ms`)

**Scenario 2: Table with verification data present**
- **Given** 2 scorecard records where `findings_verified` and `findings_refuted` are set for at least one reviewer
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the table includes additional columns: `VERIFIED`, `REFUTED`, `SURV%`
- **And** survived-skeptic rate displayed as percentage
- **And** reviewers without verification data show `-` in verification columns

**Scenario 3: Output fits within 80 columns (no verification)**
- **Given** records without verification data
- **When** the table is rendered
- **Then** the total line width does not exceed 80 characters
- **And** columns are aligned via tabwriter with 2-space padding

**Scenario 4: Single reviewer**
- **Given** only 1 scorecard record exists for the run
- **When** the user runs `atcr scorecard <run_id>`
- **Then** the table renders with header + 1 data row
- **And** alignment is correct (tabwriter does not collapse)

## Edge Cases

**Edge Case 1: Zero findings raised**
- **Given** a reviewer record with `findings_raised` = 0, `findings_corroborated` = 0, `findings_solo` = 0
- **When** the table is rendered
- **Then** corroboration rate displays as `0%` (not `NaN` or division-by-zero)
- **And** cost displays as `$0.0000`

**Edge Case 2: Very long reviewer name or model string**
- **Given** a reviewer with name `"extremely-long-reviewer-name-that-exceeds-normal-width"` and model `"very-long-model-name-with-lots-of-characters"`
- **When** the table is rendered
- **Then** tabwriter expands the column width to accommodate
- **And** other columns remain aligned

**Edge Case 3: Verification columns push width beyond 80 columns**
- **Given** records with verification data present
- **When** the full table (including verification columns) would exceed 80 characters
- **Then** the command renders verification columns in a separate section below the main table
- **Or** uses a narrower percentage format (`58%` vs `0.58`) to stay within 80 columns

**Edge Case 4: Large number of reviewers (10+)**
- **Given** 15 reviewers in a single run (unusual but possible)
- **When** the table is rendered
- **Then** all 15 rows are displayed
- **And** column alignment is consistent across all rows

## Error Conditions

**Error Scenario 1: Malformed record (missing required fields)**
- **Given** a JSONL record with `run_id` and `reviewer` but missing `findings_raised`
- **When** the table is rendered
- **Then** the missing field displays as `0` (zero value) or `-`
- **And** the row is still rendered (graceful degradation)

**Error Scenario 2: Negative values in numeric fields**
- **Given** a record with `findings_raised` = -1 (corrupt data)
- **When** the table is rendered
- **Then** the negative value is displayed as-is (data fidelity over silent correction)

## Performance Requirements
- **Render Time:** Table renders in <50ms for up to 20 reviewers
- **Memory:** Entire table built in a single `bytes.Buffer` before writing to stdout (atomic output)
- **Tabwriter Config:** `tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)` — min-width 0, tab-width 2, padding 2, padchar space, no flags

## Security Considerations
- **Output Sanitization:** Reviewer names and model strings from JSONL could contain control characters; strip or replace non-printable characters before rendering
- **No Injection:** Table output is plain text via `tabwriter`; no shell interpolation or format-string injection risk
- **Read-Only:** Rendering never modifies the scorecard store or any file

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- 3-reviewer dataset without verification data (standard output)
- 2-reviewer dataset with verification data (extended columns)
- Single reviewer dataset
- Dataset with zero-value fields
- Dataset with long strings for column width testing
- Golden files for expected table output

**Mock/Stub Requirements:**
- Inject `io.Writer` (via `bytes.Buffer`) to capture rendered output
- No filesystem access needed for render tests (pure function of `[]Record` → string)

**Test Cases:**
1. `TestRenderTable_StandardColumns` — golden file comparison for 3-reviewer output
2. `TestRenderTable_WithVerification` — extended columns present when verification data exists
3. `TestRenderTable_WidthUnder80` — standard output fits 80 columns
4. `TestRenderTable_SingleReviewer` — single-row table renders with header + 1 data row and aligned columns
5. `TestRenderTable_ZeroFindings` — corroboration rate = 0%, no division by zero
6. `TestRenderTable_MissingFields` — graceful degradation for partial records
7. `TestRenderTable_LongNames` — columns expand to fit long reviewer/model strings while remaining aligned
8. `TestRenderTable_SurvivedSkepticRate` — rate calculation: `verified / (verified + refuted)` when denominator > 0

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests passing (`go test ./cmd/atcr/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)
- [ ] Golden file tests match expected output exactly

**Story-Specific:**
- [ ] Standard table includes: reviewer, model, raised, corroborated, solo, corr%, cost, latency
- [ ] Verification columns (verified, refuted, surv%) appear ONLY when data is present
- [ ] Corroboration rate = `corroborated / raised` displayed as integer percentage
- [ ] Cost formatted as `$X.XXXX` (4 decimal places)
- [ ] Output fits within 80 columns when verification columns are absent
- [ ] `text/tabwriter` used with consistent config (0, 2, 2, ' ', 0)

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Table output visually inspected in a terminal for alignment

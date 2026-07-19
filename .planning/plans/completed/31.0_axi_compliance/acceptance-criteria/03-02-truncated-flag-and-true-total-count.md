# Acceptance Criteria: `truncated` Flag with Preserved True Total Count

**Related User Story:** [Story 3: AXI Pagination and Truncation Guarantees](../user-stories/03-axi-pagination-and-truncation-guarantees.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/report`) — payload metadata field emission | Reuses the `Truncated bool` / `json:"truncated"` naming precedent from `internal/fanout/status.go` |
| Test Framework | `go test` (standard library `testing`) | Assertions on both the emitted `truncated` field and the TOON array header's declared `N` |
| Key Dependencies | None new — same TOON/JSON encoding path as Story 1's `FormatAXI` renderer | |

### Related Files (from codebase-discovery.json)
- `internal/report/pagination.go` - create: computes and emits the `truncated` boolean alongside the line-cap step from AC 03-01, and preserves the pre-truncation element count for the array header
- `internal/fanout/status.go` - reference (lines 286-293): naming/semantic precedent for `Truncated bool` / `json:"truncated"` — this AC reuses the exact field name and true/false semantics, not a new flag
- `internal/report/render.go` - modify: `FormatAXI` computes the TOON array header's `N` from the pre-truncation element count, independent of how many rows are ultimately emitted
- `.planning/plans/active/31.0_axi_compliance/documentation/toon-format-reference.md` - reference: TOON's `key[N]{field1,field2}:` header declares the true element count, which composes with `truncated` per this document's "Declared length `N` in every array header" note

## Happy Path Scenarios
**Scenario 1: Untruncated payload reports `truncated: false`**
- **Given** an AXI-mode findings payload that renders to 120 lines (under the 500-line cap)
- **When** the payload is rendered
- **Then** the emitted payload includes `truncated: false` (or the format's documented false-case representation) and the TOON array header's `N` equals the actual number of emitted rows

**Scenario 2: Truncated payload reports `truncated: true` with the true total preserved**
- **Given** an AXI-mode findings payload containing 1,200 findings that renders to 1,200+ lines (over the 500-line cap)
- **When** the payload is rendered and truncated to 500 lines
- **Then** the emitted payload includes `truncated: true`, and the TOON array header still declares `N` = 1,200 (the true, pre-truncation element count) even though fewer rows are physically present in the output

## Edge Cases
**Edge Case 1: Header `N` never drifts to match the truncated row count**
- **Given** a payload truncated from 1,200 rows down to a smaller emitted row count
- **When** the test compares the array header's declared `N` against the number of physically emitted rows
- **Then** header `N` (1,200) is asserted to be strictly greater than the emitted row count — proving the header was computed from the pre-truncation count and not accidentally clipped alongside the rows (per the story's Risk 3 mitigation)

**Edge Case 2: Boundary payload (exactly at cap) reports `truncated: false`**
- **Given** a payload that renders to exactly 500 lines matching the default cap
- **When** the payload is rendered
- **Then** `truncated: false` is emitted (the cap boundary is inclusive, consistent with AC 03-01 Edge Case 1) and header `N` equals the full emitted row count

**Edge Case 3: Zero-findings payload**
- **Given** an empty AXI findings payload (`findings[0]{...}:`)
- **When** rendered
- **Then** `truncated: false` is emitted and header `N` is `0`

## Error Conditions
**Error Scenario 1: Missing or inconsistent `truncated` field is a rendering defect, not a runtime error**
- **Given** any AXI-mode payload
- **When** rendered
- **Then** the `truncated` field MUST be present in every AXI payload (never omitted) — its absence is caught by the AC 03-02 test suite as a test failure, not surfaced to the end user as a CLI error

## Performance Requirements
- **Response Time:** Computing the pre-truncation element count is a byproduct of the renderer's existing pass over findings/diff data (Story 1) — no additional full-payload scan is required beyond what AC 03-01's line-cap step already performs.
- **Throughput:** No measurable overhead beyond AC 03-01's truncation step; flag computation is O(1) given the count is already known before truncation.

## Security Considerations
- **Authentication/Authorization:** N/A — local rendering metadata.
- **Input Validation:** The true total count must reflect the actual reconciled findings/diff element count fed into the renderer, not a value derived from the (possibly truncated) rendered text — this AC's tests explicitly guard against the header `N` being computed post-truncation.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Synthetic payloads producing known finding counts (0, exactly-at-cap, over-cap by a large margin e.g. 1,200) so the true total is independently known and can be asserted against the header `N`.
**Mock/Stub Requirements:** None — pure function testing over renderer output with `bytes.Buffer` capture; assert via string/regex parsing of the TOON header or JSON field.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `truncated` field is present in every AXI payload (never omitted), `true` when capped and `false` otherwise
- [ ] TOON array header `N` (or compact-JSON equivalent) always reflects the true, uncapped total count
- [ ] Dedicated test asserts header `N` != emitted row count when `truncated: true` (Risk 3 regression guard)
- [ ] Field name/semantics match `internal/fanout/status.go`'s `Truncated bool` precedent exactly

**Manual Review:**
- [ ] Code reviewed and approved

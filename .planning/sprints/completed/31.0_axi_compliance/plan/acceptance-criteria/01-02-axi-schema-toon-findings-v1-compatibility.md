# Acceptance Criteria: AXI Schema Design Reconciled with `atcr-findings/v1` and TOON Conventions

**Related User Story:** [01: `--axi` Token-Dense Output Mode for `atcr review` and `atcr report`](../user-stories/01-axi-token-dense-output-mode.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Schema/encoding design realized as Go structs + an encoder function in `internal/report` | No new persisted artifact — this is a stdout-only re-encoding of existing `reconcile.JSONFinding` data |
| Test Framework | `go test` with `testify/assert`; table-driven field-mapping tests | |
| Key Dependencies | None beyond the standard library; TOON tabular-array encoding with declared length and pipe delimiter (`documentation/toon-format-reference.md`) | Evaluate but do not require the third-party Go TOON implementation referenced in `documentation/toon-format-reference.md` |

### Related Files (from codebase-discovery.json)
- `internal/report/render.go` - modify: the `renderAXI` function (added in AC 01-01) must encode the same field set exposed by `renderJSON`/`atcr-findings/v1` (severity, file:line, problem, fix, category, est_minutes, evidence, reviewers, confidence) so no field is silently dropped from the machine contract.
- `docs/findings-format.md` - modify (or companion doc create): document the AXI encoding's field-name and delimiter mapping against the existing `atcr-findings/v1` pipe-delimited columns (line 23, 39) so the two "machine format" surfaces are explicitly cross-referenced rather than left to drift independently.
- `.planning/plans/active/31.0_axi_compliance/documentation/toon-format-reference.md` - reference only: normative source for TOON tabular-array header syntax (`key[N|]{field|name}:`, line 94-98), quoting rules (line 39-45), and escape sequences (line 116-128) that the encoder must implement.

## Happy Path Scenarios
**Scenario 1: Tabular-array encoding for a uniform findings list**
- **Given** a reconciled findings list where every record has the same field set
- **When** encoded for `--axi`
- **Then** the payload uses TOON's tabular-array form (`findings[N]{severity,file,line,problem,fix,category,est_minutes,evidence,reviewers,confidence}:` followed by one row per finding), matching the reconciled `atcr-findings/v1` 9-column shape field-for-field (`docs/findings-format.md` line 36-40) rather than inventing new field names

**Scenario 2: Pipe-delimiter convergence with `atcr-findings/v1`**
- **Given** the TOON spec's alternative-delimiter feature (`key[N|]{field|name}:`, `documentation/toon-format-reference.md` line 33-37, 94-98)
- **When** the AXI tabular header is emitted
- **Then** it declares the pipe delimiter (`[N|]{...|...}:`) so the row shape is visually and structurally adjacent to the existing `SEVERITY|FILE:LINE|...` grammar, per the plan's anti-fragmentation risk mitigation (story `Potential Risks` table)

**Scenario 3: axi.md design-tension resolutions are explicitly recorded with the schema**
- **Given** the schema decision must resolve axi.md Principle 2 (3–4 default fields) against the findings stream's 8–9-column width, and Principle 4 (pre-computed aggregates) against a row-level findings payload (`documentation/axi-design-principles.md`: Principles 2 and 4)
- **When** the AXI schema is finalized during sprint design
- **Then** the decision record (sprint design note or schema source comment) explicitly states: (a) the full-width field set is retained because pipe-delimited TOON rows are already token-lean — the Principle 2 deviation justified per the story's Technical Considerations — and (b) Principle 4's aggregate guidance is honored via the array header's declared true total `N` plus run metadata (review id, dir, agent/finding counts) carried in the review-path payload (AC 01-03), rather than a separate aggregation pass

## Edge Cases
**Edge Case 1: `verification` block present (VERIFIED confidence, skeptic, notes)**
- **Given** a finding that carries a non-nil `Verification` block (Epic 3.0 confidence-v2 field, `docs/findings-format.md` line 94-118)
- **When** encoded for `--axi`
- **Then** the verdict/skeptic/notes are represented as additive nested fields (or a `verification` sub-object per TOON's object-nesting rule) rather than being dropped, so an axi consumer sees the same verification signal a `--format json` consumer would

**Edge Case 2: `evidence_exec` reproduction block present**
- **Given** a finding stamped with `evidence_exec` (Epic 11.0, `docs/findings-format.md` line 120-124)
- **When** encoded for `--axi`
- **Then** the command/exit_code/output_excerpt are carried through as additive fields rather than silently omitted, keeping the axi payload a complete re-encoding (not a lossy subset) of the JSON contract

**Edge Case 3: A field value that is itself a TOON reserved token (`true`, `false`, `null`, or a numeric-looking string like `"42"`)**
- **Given** a `Category` or `Problem` value that happens to equal `"true"` or look like a number
- **When** encoded for `--axi`
- **Then** the value is quoted per the TOON must-quote rule (`documentation/toon-format-reference.md` line 41) so a downstream parser does not misinterpret a string field as a boolean/null/number

## Error Conditions
**Error Scenario 1: Field-count mismatch between the TOON header and a data row**
- **Given** an internal encoder bug that appends a row with a different column count than the declared header
- **When** `renderAXI` runs (as a defensive internal invariant, not a user-facing input)
- **Then** the encoder must panic/error deterministically in a unit test rather than silently emit a malformed payload (guarded by a table-driven test asserting `len(row) == len(header fields)` for every sample)
- Error message: internal invariant violation, e.g. `"axi encoder: row has %d columns, header declares %d"`

## Performance Requirements
- **Response Time:** Schema mapping adds no additional pass over the findings slice beyond the single encoding pass already required by AC 01-01 (no O(n²) field lookups).
- **Throughput:** N/A — in-process encoding, not a service boundary.

## Security Considerations
- **Authentication/Authorization:** None — schema design has no auth surface.
- **Input Validation:** Every free-text field (`Problem`, `Fix`, `Evidence`, `Verification.Notes`) must pass through the same quoting/escaping path validated in AC 01-01, so the schema-compatibility work does not introduce a second, divergent escaping implementation for the same field types.

## Test Implementation Guidance
**Test Type:** UNIT (field-mapping table-driven tests) + documentation cross-check
**Test Data Requirements:** A findings fixture that includes a `Verification` block, an `EvidenceExec` block, and a value that collides with a TOON reserved token, to exercise all three edge cases in one table-driven test.
**Mock/Stub Requirements:** None.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/report/...`)
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] AXI payload's field set is verified (via test or manual diff) to be a superset of the `atcr-findings/v1` reconciled 9-column contract — no field silently dropped
- [x] Pipe delimiter is used for the AXI tabular-array header, matching the `atcr-findings/v1` convergence decision recorded in the story
- [x] axi.md Principle 2 (default-field-count) and Principle 4 (pre-computed aggregates) tension resolutions are recorded with the schema decision, per the story's Technical Considerations and `documentation/axi-design-principles.md`
- [x] `docs/findings-format.md` (or a sibling doc) cross-references the AXI schema against `atcr-findings/v1` so the two machine-format surfaces do not drift independently
- [x] Verification and evidence_exec blocks round-trip into the AXI payload when present

**Manual Review:**
- [x] Code reviewed and approved

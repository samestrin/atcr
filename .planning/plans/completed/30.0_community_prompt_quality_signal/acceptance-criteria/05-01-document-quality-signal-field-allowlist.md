# Acceptance Criteria: Document the Quality-Signal Payload's Exact Field Allowlist

**Related User Story:** [05: Document the Quality-Signal Telemetry Contract](../user-stories/05-document-quality-signal-telemetry-contract.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Markdown documentation (`docs/telemetry.md`) | New section, sibling to the existing "Usage ping schema" table |
| Test Framework | Manual cross-check + doc-lint (markdown link/table syntax) | No executable test suite for prose; verification is a field-by-field diff against shipped Go source |
| Key Dependencies | `internal/telemetry/quality_signal.go` (Story 1 struct), its allowlist regression test | Doc content is derived from these, never invented |

## Related Files
- `docs/telemetry.md` - modify: add a new "Community prompt quality signal" section (or equivalently named, placed after "Persona Leaderboard data" / before or after "Cloud sync (`--sync-cloud`)") containing a field table mirroring the existing "Usage ping schema" table's format (`Field | Type | Example | Meaning`)
- `internal/telemetry/quality_signal.go` - reference only: the shipped `QualitySignal` struct (Story 1) is the sole source of truth for field names/types/count — the doc table must list exactly these fields, no more, no fewer
- `internal/telemetry/quality_signal_test.go` (or equivalent allowlist regression test named per Story 1, e.g. mirroring `TestClient_Send_PayloadHasExactlyFourAllowlistedKeys` in `internal/telemetry/client_test.go`) - reference only: cross-check that the test's asserted key set matches the doc table exactly
- `internal/localdebt/qualitysignal.go` (or wherever Story 1's aggregation type lives) - reference only: confirms the field *meanings* (e.g. what "dismissed_count" counts) documented in the new table are accurate

### Related Files (from codebase-discovery.json)

- `docs/telemetry.md` - update: new "Community prompt quality signal" section with the field-allowlist table mirroring the "Usage ping schema" table (`:26`)

## Happy Path Scenarios
**Scenario 1: Field table lists every allowlisted field with correct name and type**
- **Given** the shipped `QualitySignal` struct in `internal/telemetry/quality_signal.go` has exactly 4 fields (e.g. `persona_id_hash string`, `model string`, `dismissed_count int`, `confirmed_count int`)
- **When** a reviewer reads the new section's field table in `docs/telemetry.md`
- **Then** the table lists exactly 4 rows, one per struct field, with the JSON key name (not the Go field name) and correct type, matching the struct field-for-field

**Scenario 2: Table format matches the existing usage-ping schema table's style**
- **Given** the existing "Usage ping schema" table uses `Field | Type | Example | Meaning` columns
- **When** the new quality-signal field table is added
- **Then** it uses the same column structure and phrasing style, so a reader who has already read the usage-ping section immediately recognizes the pattern

**Scenario 3: Doc explicitly states the payload is its own allowlist, not a superset of `Event`**
- **Given** the story's Constraints section forbids describing the quality-signal payload as an extension of the existing 4-field `Event` struct
- **When** the new section is read
- **Then** it contains an explicit sentence stating the quality-signal payload is a separately-defined, separately-tested struct — distinct from (not layered on top of) the usage-ping `Event` schema

## Edge Cases
**Edge Case 1: A field is renamed during Stories 1-4 implementation after this story is drafted**
- **Given** the plan-stage design used placeholder names (e.g. `persona_id_hash`) that may change during implementation
- **When** the doc is finalized
- **Then** the doc author re-reads the actual shipped `internal/telemetry/quality_signal.go` and its allowlist test immediately before finalizing, and uses the real shipped names — not the plan-stage placeholder — if they differ

**Edge Case 2: Zero-value counts must be documented as always-present, not omitted**
- **Given** Story 1's AC states zero-value counts serialize as `0` rather than being dropped (no `omitempty`)
- **When** the field table's "Meaning" column describes `dismissed_count`/`confirmed_count`
- **Then** it notes that a zero count is always present in the payload (distinguishing "zero dismissals" from "field absent"), matching the existing usage-ping doc's precedent of calling out field-population nuances (e.g. the `reconcile_run` empty-`lang`/zero-`lines` note)

## Error Conditions
**Error Scenario 1: Documented field count mismatches the shipped struct**
- **Given** a reviewer diffs the doc table against `internal/telemetry/quality_signal.go` and its allowlist test
- **When** a discrepancy is found (a field documented that doesn't exist, or a shipped field left undocumented)
- **Then** this AC is not met — the story's Measurable success criterion (zero discrepancies, verifiable in under 5 minutes) fails, and the doc must be corrected before merge
- HTTP status / error code: not applicable (documentation-only; failure mode is a review-gate rejection, not a runtime error)

## Performance Requirements
- **Response Time:** Not applicable — static documentation, no runtime behavior.
- **Throughput:** Not applicable.
- **Review latency:** A reviewer must be able to complete the field-by-field cross-check against source in under 5 minutes (per the story's Measurable criterion) — achieved by keeping the table compact (4 rows) and placing it adjacent to a direct reference to the source file path.

## Security Considerations
- **Sensitive information:** The doc must not reproduce example persona names, model names, or counter values that could be mistaken for real telemetry data from a live deployment — examples must be clearly synthetic/illustrative (matching the existing usage-ping table's synthetic `Example` column values).
- **No invented claims:** The doc must not assert any field, behavior, or guarantee beyond what `internal/telemetry/quality_signal.go` and its allowlist test actually enforce — every sentence in the new section must be traceable to shipped code or an existing, already-documented precedent (e.g. the `HashPersonaID` caveat).

## Test Implementation Guidance
**Test Type:** MANUAL (doc-accuracy review) + doc-lint (markdown table/link syntax check, e.g. `markdownlint` or the project's existing doc-lint step if any)
**Test Data Requirements:** The shipped `internal/telemetry/quality_signal.go` source and its allowlist regression test, read side-by-side with the new `docs/telemetry.md` section during review.
**Mock/Stub Requirements:** None — this is a static content comparison, not an executable test.

## Definition of Done
**Auto-Verified:**
- [ ] Markdown renders without syntax errors (table, headers, links)
- [ ] No broken internal links introduced in `docs/telemetry.md`
- [ ] `go build ./...` and `go test ./...` still pass (no source changed by this story, but the repo must remain green)

**Story-Specific:**
- [ ] New field table lists exactly the fields present in the shipped `QualitySignal` struct — no more, no fewer
- [ ] Table format mirrors the existing "Usage ping schema" table's column structure
- [ ] New section explicitly states the payload is its own separately-tested allowlist, not an extension of `Event`
- [ ] Zero-value count fields are documented as always-present, not omitted

**Manual Review:**
- [ ] Code reviewed and approved

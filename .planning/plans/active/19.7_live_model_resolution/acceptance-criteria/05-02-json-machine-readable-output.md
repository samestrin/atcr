# Acceptance Criteria: `--json` Machine-Readable Output Shape

**Related User Story:** [05: `atcr models check` Drift Report](../user-stories/05-atcr-models-check-drift-report.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Cobra `--json` bool flag on `check` + shared internal drift-report struct | Both renderers (table and JSON) derive from one internal data structure per Risk Mitigation in the user story |
| Test Framework | Go `testing` + `testify` + `encoding/json` | Table-driven, asserting decoded JSON field values, not raw string matching |
| Key Dependencies | `encoding/json` (stdlib), `github.com/spf13/cobra` | No new external dependency |

## Related Files
- `cmd/atcr/models.go` - create: define an internal `driftFinding` (or equivalent) struct with fields covering `persona`, `condition`, `current_slug`, and condition-specific fields (`suggested_slug`/`family`/`channel` for newer-member, `expiration_date` for deprecation); `check`'s `RunE` builds a `[]driftFinding` once and renders it either as the human-readable table (AC 05-01) or as JSON (this AC) depending on the `--json` flag — never two independently-computed code paths.
- `cmd/atcr/models_test.go` - create: JSON-mode tests that unmarshal `--json` stdout into `[]driftFinding` (or a local test-side struct) and assert per-condition field values, plus a same-input parity test asserting the JSON and human-readable renderers report the identical set of personas/conditions.
- `cmd/atcr/personas.go` - reference: existing precedent for an optional machine-readable flag gating table vs. structured output on the same command, mirrored here for `--json`.

## Happy Path Scenarios
**Scenario 1: `--json` emits a JSON array with one object per condition**
- **Given** installed personas producing one newer-member finding (`anthony`), one deprecation finding (`gene`), and one missing-slug finding (`milo`)
- **When** `atcr models check --json` runs
- **Then** stdout is valid JSON that decodes to an array of exactly three objects, each carrying a `persona` and `condition` field identifying which of the three drift types it represents

**Scenario 2: Newer-member JSON object carries slug/family/channel fields**
- **Given** the `anthony` newer-member finding from Scenario 1
- **When** the JSON output is decoded
- **Then** the corresponding object's `condition` is the newer-member condition string, `current_slug` is `anthropic/claude-opus-4.8`, and a suggested/target slug field is present with the newer catalog slug (`anthropic/claude-opus-5.0`), alongside the family and channel context in dedicated fields

**Scenario 3: Deprecation and missing JSON objects carry their condition-specific fields**
- **Given** the `gene` deprecation and `milo` missing findings from Scenario 1
- **When** the JSON output is decoded
- **Then** the `gene` object carries `current_slug` plus the non-null `expiration_date`, and the `milo` object carries `current_slug` with no expiration or suggested-slug field required (or those fields rendered null/omitted rather than fabricated)

## Edge Cases
**Edge Case 1: No conditions found in `--json` mode**
- **Given** no drift, deprecation, or missing conditions across all installed personas
- **When** `atcr models check --json` runs
- **Then** stdout is a valid, well-formed empty JSON array (`[]`), not an empty string, `null`, or a non-JSON "no issues" message — machine consumers must be able to `json.Unmarshal` the output unconditionally

**Edge Case 2: JSON and human-readable outputs report identical findings for the same input**
- **Given** an identical installed-persona/lock fixture run twice — once with `--json`, once without
- **Then** the set of (persona, condition) pairs reported is identical between both output modes, since both are derived from the same internal drift-report structure

**Edge Case 3: A persona with multiple conditions produces multiple JSON objects**
- **Given** a persona with both a deprecation and a newer-member condition
- **When** `atcr models check --json` runs
- **Then** the JSON array contains two distinct objects for that persona (one per condition), each fully populated for its own condition type, rather than one object conflating both

## Error Conditions
**Error Scenario 1: `--json` combined with a command/usage failure still exits non-zero without emitting partial/invalid JSON**
- **Given** a usage error occurs before the drift report can be computed (see AC 05-03 for exit-code specifics)
- **When** `atcr models check --json` is invoked in that failing state
- **Then** stdout does not contain a truncated or malformed JSON fragment; any error detail is written to `cmd.ErrOrStderr()` instead, keeping stdout either empty or a well-formed JSON value

## Performance Requirements
- **Response Time:** JSON serialization adds negligible overhead (single `encoding/json.Marshal` call) over the human-readable path for typical installed-persona counts.
- **Throughput:** Single marshal pass over the already-computed `[]driftFinding` slice; no per-finding re-computation between the two output modes.

## Security Considerations
- **Authentication/Authorization:** Not applicable — same read-only, local-only scope as AC 05-01.
- **Input Validation:** Field values (slugs, persona names, dates) are marshaled as opaque strings via standard library JSON encoding, which safely escapes any control characters — no custom string concatenation into the JSON output.

## Test Implementation Guidance
**Test Type:** UNIT (struct marshaling) + INTEGRATION (Cobra `--json` flag end-to-end, decoding real command stdout)
**Test Data Requirements:** Same fixture set as AC 05-01 (up-to-date, newer-member, deprecated, missing, multi-condition, and empty-result installed-persona states), reused here with JSON-decode assertions instead of string-match assertions.
**Mock/Stub Requirements:** None beyond the filesystem fixtures already established in AC 05-01; no network mocking needed.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `--json` flag exists on `check` and emits a valid JSON array decodable via `encoding/json`
- [ ] Each condition type's JSON object carries its documented condition-specific fields (`current_slug` always; `suggested_slug`/`family`/`channel` for newer-member; `expiration_date` for deprecation)
- [ ] An empty-findings run emits `[]`, never `null` or blank stdout
- [ ] JSON and human-readable output modes report an identical (persona, condition) set for the same input, proving both derive from one shared internal structure

**Manual Review:**
- [ ] Code reviewed and approved

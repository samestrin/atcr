# Acceptance Criteria: SARIF Base Document Structure

**Related User Story:** [01: SARIF Formatter Core](../user-stories/01-sarif-formatter-core.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package/function (`internal/report/sarif.go`, new file) | `renderSarif(w io.Writer, findings []reconcile.JSONFinding) error`, mirroring `renderJSON`'s shape (`internal/report/render.go:65-77`) |
| Test Framework | `go test` (table-driven + golden-file), `encoding/json` round-trip via `json.Valid`/`json.Unmarshal` | Golden case added to `TestRender_GoldenFiles` (`internal/report/render_test.go:69-93`) |
| Key Dependencies | stdlib `encoding/json` only; `github.com/google/jsonschema-go` (already in `go.mod`) optionally used in tests to validate structural conformance against a local SARIF 2.1.0 schema fixture | No new runtime dependency — hand-rolled struct tree, per story constraint |

## Related Files
- `internal/report/sarif.go` - create: defines the SARIF struct tree (`sarifLog`, `sarifRun`, `sarifTool`, `sarifDriver`, `sarifResult`, etc. — `sarifRule` is AC 01-03's concern) and `renderSarif`, applying the same nil-slice guard and `json.MarshalIndent(..., "", "  ")` + trailing-newline convention `renderJSON` uses.
- `internal/report/sarif_test.go` - create: table-driven unit tests asserting top-level document shape (`$schema`, `version`, `runs[]`, `tool.driver.name`, `results[]` presence) for empty and non-empty findings inputs.
- `internal/report/render_test.go` - modify: add a `{"sarif", FormatSarif, "report.sarif.json"}` entry to `goldenCases` (line 59-67) so `TestRender_GoldenFiles` exercises SARIF alongside md/json/checklist.
- `internal/report/testdata/report.sarif.json` - create: golden fixture generated via `go test ./internal/report -update`, driven by the existing `sample()` fixture (two findings: CRITICAL/security, LOW/style).

## Happy Path Scenarios
**Scenario 1: non-empty findings produce a valid top-level SARIF document**
- **Given** the `sample()` fixture (two findings)
- **When** `renderSarif(w, sample())` is called
- **Then** `w` contains a single JSON document where `$schema` is the SARIF 2.1.0 schema URI, `version == "2.1.0"`, `runs` is a non-empty array, `runs[0].tool.driver.name == "atcr"`, and `runs[0].results` is a JSON array with exactly 2 entries

**Scenario 2: empty findings still produce a structurally valid document**
- **Given** `findings = []reconcile.JSONFinding{}` (or `nil`)
- **When** `renderSarif(w, findings)` is called
- **Then** the output still has `$schema`, `version: "2.1.0"`, `runs[]` non-empty, `runs[0].tool.driver.name == "atcr"`, and `runs[0].results` serializes as `[]` — never `null`

**Scenario 3: output round-trips through JSON unmarshal**
- **Given** any `renderSarif` output (empty or non-empty findings)
- **When** the bytes are passed to `json.Valid()` and then `json.Unmarshal` into a generic `map[string]any` or the local struct tree
- **Then** both succeed with no error, confirming syntactic JSON validity

## Edge Cases
**Edge Case 1: nil findings slice does not panic and does not serialize as null**
- **Given** `findings == nil`
- **When** `renderSarif(w, nil)` is called
- **Then** `runs[0].results` is `[]`, not `null` — mirroring the `renderJSON` nil-slice guard (`internal/report/render.go:68-70`) applied to the SARIF results array

**Edge Case 2: output ordering is deterministic across repeated calls**
- **Given** the same `findings` slice passed to `renderSarif` twice
- **When** both outputs are compared byte-for-byte
- **Then** they are identical — key ordering (struct field order via `encoding/json`) and any derived array ordering (e.g. rule iteration order, AC 01-03) is stable, so golden-file tests are reproducible

**Edge Case 3: a single finding with all optional JSONFinding fields empty still renders**
- **Given** a `reconcile.JSONFinding{Severity: "LOW", File: "x.go", Line: 1, Problem: "p", Category: "misc"}` with `Fix`, `Evidence`, `Reviewers`, `Confidence` left zero-valued
- **When** `renderSarif` is called with this single finding
- **Then** the output still validates as JSON and produces exactly one `results[]` entry with no panic or missing required SARIF object (e.g. `message.text` falls back to a non-empty string rather than emitting an empty/absent required field)

## Error Conditions
**Error Scenario 1: write failure propagates**
- **Given** an `io.Writer` that returns an error on `Write` (e.g. a closed pipe or a test double)
- **When** `renderSarif(w, findings)` is called
- Error message: the underlying writer error, wrapped or passed through unchanged (matching `renderJSON`'s `_, err = w.Write(...); return err` pattern)
- Go error type: standard `error`, no panic

**Error Scenario 2: JSON marshal failure (defensive, expected unreachable)**
- **Given** the struct tree is composed only of marshalable Go primitives/structs (no channels, funcs, or cyclic references)
- **When** `json.MarshalIndent` is called internally
- **Then** no error path is realistically reachable in production use; if it were, `renderSarif` returns the wrapped marshal error rather than panicking (same contract as `renderJSON`)

## Performance Requirements
- **Response Time:** `renderSarif` completes in O(n) over the findings slice (single pass to build `results[]`, single pass to collect distinct categories for `rules[]` per AC 01-03) — no quadratic behavior for typical review sizes (tens to low hundreds of findings).
- **Throughput:** N/A (single-process CLI/MCP call, not a service); memory usage scales linearly with input size, consistent with `renderJSON`.

## Security Considerations
- **Authentication/Authorization:** N/A — local rendering over already-trusted, already-validated `reconcile.JSONFinding` records; no new trust boundary crossed.
- **Input Validation:** Free-text fields (`Problem`, `File`, `Category`) are attacker-influenced (sourced from LLM reviewer output) and must be placed into SARIF string fields via `encoding/json`'s standard escaping (which HTML/JSON-escapes control characters and quotes) — no raw string concatenation into the JSON output, preventing JSON injection. Unlike the markdown renderer, no HTML-escaping (`esc()`) is needed since the sink is JSON, not HTML/markdown; `encoding/json` alone is the correct and sufficient escaping mechanism here.

## Test Implementation Guidance
**Test Type:** UNIT + golden-file (via `TestRender_GoldenFiles`)
**Test Data Requirements:** Reuses `sample()` (two findings) for the golden case; a dedicated empty-slice and nil-slice case for the `results[]` non-null assertion; a minimal single-finding fixture for Edge Case 3.
**Mock/Stub Requirements:** An `io.Writer` stub that errors on `Write` for Error Scenario 1 (e.g. a small local type implementing `io.Writer` that always returns an error) — no external service mocking needed.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/report/...`)
- [ ] No linting errors
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `renderSarif` output has `$schema`, `version: "2.1.0"`, non-empty `runs[]`, `runs[0].tool.driver.name == "atcr"` for both empty and non-empty findings
- [ ] `runs[0].results` serializes as `[]` (never `null`) when findings is nil/empty
- [ ] `TestRender_GoldenFiles` passes for the new `"sarif"` case against `testdata/report.sarif.json`
- [ ] Repeated calls with identical input produce byte-identical output (determinism)

**Manual Review:**
- [ ] Code reviewed and approved

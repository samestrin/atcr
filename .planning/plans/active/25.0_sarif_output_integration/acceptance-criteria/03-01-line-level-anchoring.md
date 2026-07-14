# Acceptance Criteria: Line-Level Anchoring (URI Pass-Through + Line>0 Region)

**Related User Story:** [03: SARIF Line/File Anchoring](../user-stories/03-sarif-line-file-anchoring.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (`sarifLocation` helper) | Pure data-mapping, no I/O |
| Test Framework | `go test` (table-driven) | Follows `internal/report/render_test.go` conventions |
| Key Dependencies | `internal/reconcile` (`reconcile.JSONFinding`) | No new dependency introduced |

## Related Files
- `internal/report/sarif.go` - create: adds `sarifLocation(f reconcile.JSONFinding) sarifLocationObj` (or equivalent, named to match Story 1's struct tree) that builds `physicalLocation.artifactLocation.uri` from `f.File` unmodified, and for `f.Line > 0` sets `region.startLine = region.endLine = f.Line` with synthesized non-zero `startColumn`/`endColumn`.
- `internal/report/sarif_test.go` - create: table-driven test cases asserting exact `artifactLocation.uri` and `region` field values for `Line > 0` inputs.
- `internal/reconcile/emit.go` - reference only: source of `reconcile.JSONFinding.File` (string, repo-root-relative) and `.Line` (int) fields consumed by `sarifLocation`.
- `internal/ghaction/render.go` - reference only: existing `location(f reconcile.JSONFinding) string` helper (lines 103-108) is the precedent for the `Line<=0` special-case pattern this story's helper mirrors (for AC 03-02), establishing that `File` is already repo-root-relative by the time it reaches the report layer.

## Happy Path Scenarios
**Scenario 1: Finding with a valid positive line number anchors to that exact line**
- **Given** a `reconcile.JSONFinding` with `File: "internal/foo/bar.go"` and `Line: 42`
- **When** `sarifLocation(f)` is called
- **Then** the returned `physicalLocation.artifactLocation.uri` equals `"internal/foo/bar.go"` exactly, and `region.startLine == region.endLine == 42`, with `region.startColumn` and `region.endColumn` both populated to a defined non-zero value (e.g. `1`)

**Scenario 2: `artifactLocation.uri` passes through `File` unmodified regardless of path shape**
- **Given** findings with varied but already repo-root-relative `File` values (e.g. `"cmd/atcr/main.go"`, `"internal/report/sarif.go"`)
- **When** `sarifLocation(f)` is called for each
- **Then** `artifactLocation.uri` exactly equals the input `File` string in every case — no `./` prefix added or stripped, no path normalization, no absolute-path conversion

**Scenario 3: Deterministic output across repeated calls**
- **Given** the same `reconcile.JSONFinding` (`File` + `Line > 0`) passed to `sarifLocation` multiple times
- **When** the results are compared
- **Then** `artifactLocation.uri` and all four `region` fields are byte-identical across calls (no timestamp- or run-relative data leaks in), preserving GitHub `partialFingerprints` deduplication stability

## Edge Cases
**Edge Case 1: Line number at the very first line of a file (`Line == 1`)**
- **Given** a finding with `Line: 1` (a real, valid line-1 finding, not a fallback)
- **When** `sarifLocation(f)` is called
- **Then** `region.startLine == region.endLine == 1`, indistinguishable in shape from any other `Line > 0` case (this is a coincidental overlap with AC 03-02's fallback value, not a special case — verified by a dedicated test with `Line: 1` alongside the `Line <= 0` cases to confirm no cross-contamination in the implementation)

**Edge Case 2: Very large line number**
- **Given** a finding with `Line: 999999` (large file)
- **When** `sarifLocation(f)` is called
- **Then** `region.startLine == region.endLine == 999999` with no integer overflow or truncation

**Edge Case 3: Empty or unusual `File` string**
- **Given** a finding with `File: ""` (defensive case; should not occur upstream but must not panic)
- **When** `sarifLocation(f)` is called
- **Then** `artifactLocation.uri == ""` is passed through without panicking or substituting a placeholder — this story does not add validation/defaulting for `File`, only pass-through

## Error Conditions
**Error Scenario 1: No error path exists for this pure mapping function**
- `sarifLocation` is a pure function with no fallible operations (no I/O, no parsing, no external calls); it does not return an `error`
- Malformed but non-empty `File` strings (e.g. containing unexpected characters) are passed through unmodified per Scenario 2 — this is a documented non-goal, not a defect

**Error Scenario 2: Negative or zero `Line` must NOT be handled by this AC's code path**
- Any `Line <= 0` value must route to the fallback logic covered by AC 03-02, not silently produce `region.startLine == 0` or `region.startLine < 0`
- Enforced by a test asserting `sarifLocation` never returns `region.startLine <= 0` for any input (covering both this AC's `Line > 0` cases and AC 03-02's fallback cases in the same table)

## Performance Requirements
- **Response Time:** O(1) per finding; no measurable overhead — pure struct construction with no loops, no I/O, no reflection
- **Throughput:** Must not introduce a per-finding allocation pattern that would degrade `renderSarif` below O(n) total for n findings

## Security Considerations
- **Authentication/Authorization:** Not applicable — pure in-process data transformation, no network/auth surface
- **Input Validation:** `File` is passed through unmodified (no sanitization performed or required at this layer); no path traversal risk is introduced since no filesystem access occurs — `sarifLocation` never opens, reads, or resolves the path, it only copies the string into JSON output

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `reconcile.JSONFinding` fixtures with `File` set to representative repo-root-relative paths (e.g. `"internal/report/sarif.go"`) and `Line` set to `1`, `42`, and a large value (`999999`); reuse the fixture-construction style already present in `render_test.go`'s table-driven tests
**Mock/Stub Requirements:** None — `sarifLocation` has no external dependencies to mock; test calls it directly with constructed `reconcile.JSONFinding` values

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/report/...`)
- [ ] No linting errors (`golangci-lint` / project lint target)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `sarifLocation` sets `artifactLocation.uri` to `f.File` unmodified for all tested `File` values
- [ ] For `Line > 0`, `region.startLine == region.endLine == f.Line` exactly, verified for at least three distinct positive `Line` values including `1`
- [ ] `region.startColumn`/`endColumn` are always non-zero for `Line > 0` cases
- [ ] Table-driven test in `sarif_test.go` asserts exact field values (not just non-nil presence) for `Line > 0` cases

**Manual Review:**
- [ ] Code reviewed and approved

# Acceptance Criteria: File-Level Fallback Anchoring (Line<=0 Synthesized Region)

**Related User Story:** [03: SARIF Line/File Anchoring](../user-stories/03-sarif-line-file-anchoring.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (`sarifLocation` helper, `Line<=0` branch) | Pure data-mapping, no I/O |
| Test Framework | `go test` (table-driven) | Follows `internal/report/render_test.go` conventions |
| Key Dependencies | `internal/reconcile` (`reconcile.JSONFinding`) | No new dependency introduced |

### Related Files (from codebase-discovery.json)

- [`internal/report/sarif.go`](../../../../../internal/report/sarif.go) — create: the `Line <= 0` branch of `sarifLocation(f reconcile.JSONFinding) sarifLocationObj` that synthesizes `region.startLine=1, startColumn=1, endLine=1, endColumn=1` instead of omitting `region` or leaving fields zero-valued, covering both `Line == 0` and `Line < 0` inputs with a single `<= 0` boundary check.
- [`internal/report/sarif_test.go`](../../../../../internal/report/sarif_test.go) — create: table-driven test cases for `Line == 0` and `Line < 0` as two distinct rows, both asserting the exact synthesized `1,1,1,1` region values.
- [`internal/ghaction/render.go`](../../../../../internal/ghaction/render.go) — reference only: the existing `location(f reconcile.JSONFinding) string` helper ([`internal/ghaction/render.go:103-108`](../../../../../internal/ghaction/render.go)) is the cited precedent for the `Line<=0` boundary condition (same `<=` comparison, not just `== 0`) — SARIF's `region` has no "omit the line" equivalent, so this AC synthesizes coordinates instead of omitting the field.
- [`internal/report/testdata/report.sarif.json`](../../../../../internal/report/testdata/report.sarif.json) — modify: golden fixture (introduced by Story 1) should include at least one file-level (`Line<=0`) finding alongside line-level findings so `TestRender_GoldenFiles`-style coverage exercises this fallback end-to-end, not just via the isolated unit test.

### Technical References

- [SARIF 2.1.0 Schema Reference](../documentation/sarif-schema-reference.md)
- [GitHub Code Scanning SARIF Integration Constraints](../documentation/github-code-scanning-integration.md)

## Happy Path Scenarios
**Scenario 1: Finding with `Line == 0` (file-level, no line data) gets the synthesized fallback**
- **Given** a `reconcile.JSONFinding` with `File: "internal/foo/bar.go"` and `Line: 0`
- **When** `sarifLocation(f)` is called
- **Then** the returned `region.startLine == 1`, `region.startColumn == 1`, `region.endLine == 1`, `region.endColumn == 1`, and `artifactLocation.uri == "internal/foo/bar.go"` (unaffected by the fallback)

**Scenario 2: Finding with negative `Line` also gets the synthesized fallback**
- **Given** a `reconcile.JSONFinding` with `File: "internal/foo/bar.go"` and `Line: -1`
- **When** `sarifLocation(f)` is called
- **Then** the returned `region` is identical to Scenario 1's (`1,1,1,1`) — the boundary check is `Line <= 0`, not `Line == 0`, so negative values are not missed

**Scenario 3: `region` is fully populated (never omitted or partially populated) for fallback findings**
- **Given** any finding with `Line <= 0`
- **When** the SARIF result is serialized to JSON
- **Then** the `region` object is present with all four fields (`startLine`, `startColumn`, `endLine`, `endColumn`) as non-zero integers — satisfying GitHub Code Scanning's all-four-fields-required-to-display constraint, in contrast to the base SARIF spec's allowance to omit `region` entirely for file-level results

## Edge Cases
**Edge Case 1: Large-magnitude negative `Line` value**
- **Given** a finding with `Line: -999` (defensive; should not occur upstream but must not produce a different fallback)
- **When** `sarifLocation(f)` is called
- **Then** the fallback `1,1,1,1` region is produced identically to `Line: -1` or `Line: 0` — the fallback value does not derive from the magnitude of a negative `Line`

**Edge Case 2: Fallback coordinates collide across multiple distinct file-level findings in the same file**
- **Given** two or more findings for the same `File` value, all with `Line <= 0`
- **When** each is passed through `sarifLocation`
- **Then** each independently produces `region: {1,1,1,1}` — this is an accepted, documented trade-off (per the story's Potential Risks table): stacked/overlapping display in GitHub is preferable to non-display, and no de-duplication or coordinate-spreading logic is introduced by this AC

**Edge Case 3: Boundary value `Line == 1` is NOT treated as a fallback case**
- **Given** a finding with `Line: 1` (a real, valid positive line number)
- **When** `sarifLocation(f)` is called
- **Then** it is routed through the normal `Line > 0` path (AC 03-01), not the fallback branch — verified by a test asserting the code path taken (e.g. via a distinguishing side channel in the test, or simply confirming both `Line: 1` and `Line: 0` produce the same numeric region but are exercised by logically separate table rows so a future off-by-one boundary regression is caught if the comparison operator changes from `<=` to `<`)

## Error Conditions
**Error Scenario 1: `Line <= 0` boundary mis-implemented as `Line == 0` only**
- Defect scenario (must be prevented, not a runtime error): if the implementation checks `Line == 0` instead of `Line <= 0`, negative `Line` values would fall through to the `Line > 0` branch and produce an invalid `region.startLine <= 0`
- Test requirement: an explicit `Line < 0` table row (distinct from `Line == 0`) is mandatory per the story's Success Criteria and Potential Risks table — this is the primary regression this AC's tests must catch
- No runtime error/panic occurs either way; the failure mode is silently-invalid SARIF output that GitHub would reject or fail to render, so correctness is enforced entirely through test assertions rather than a Go `error` return

**Error Scenario 2: Region left zero-valued or `nil` instead of synthesized**
- Defect scenario: if `sarifLocation` omits `region` or leaves any of the four fields at Go's zero value (`0`) instead of the synthesized `1`, GitHub Code Scanning silently fails to render the result (no error surfaces anywhere in the pipeline)
- Test requirement: assert exact field values (`== 1`, not `!= 0` or "present") for all four `region` fields in every `Line <= 0` test row

## Performance Requirements
- **Response Time:** O(1) per finding; identical performance characteristics to the `Line > 0` path (single comparison plus constant struct construction)
- **Throughput:** No additional allocation or branching overhead beyond a single `if f.Line <= 0` check per finding

## Security Considerations
- **Authentication/Authorization:** Not applicable — pure in-process data transformation, no network/auth surface
- **Input Validation:** No validation is needed or performed on `Line`'s sign/magnitude beyond the `<= 0` comparison; synthesized fallback values are fixed constants (`1`), not derived from untrusted input, so there is no injection or overflow surface

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `reconcile.JSONFinding` fixtures with `Line` set to `0`, `-1`, and `-999` as three distinct table rows (not collapsed into one), each asserting exact `region` field values of `1,1,1,1`; include a golden-fixture-level fixture (`internal/report/testdata/report.sarif.json`) update with at least one `Line<=0` finding per the story's Integration Points note
**Mock/Stub Requirements:** None — `sarifLocation` has no external dependencies to mock; test calls it directly with constructed `reconcile.JSONFinding` values

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/report/...`)
- [x] No linting errors (`golangci-lint` / project lint target)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `Line == 0` and `Line < 0` both independently verified to produce `region: {startLine:1, startColumn:1, endLine:1, endColumn:1}` via distinct table rows
- [x] `artifactLocation.uri` remains `f.File` unmodified in fallback cases (fallback affects only `region`, never `artifactLocation`)
- [x] `region` is never omitted, `nil`, or partially populated for `Line<=0` findings — all four fields explicitly asserted
- [x] Golden fixture (`report.sarif.json`) includes at least one `Line<=0` finding exercising this fallback end-to-end, if the fixture exists per Story 1's scope

**Manual Review:**
- [x] Code reviewed and approved

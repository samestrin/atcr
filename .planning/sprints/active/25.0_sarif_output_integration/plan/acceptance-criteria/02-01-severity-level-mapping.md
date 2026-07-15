# Acceptance Criteria: Severity-to-SARIF-Level Mapping Function

**Related User Story:** [02: Severity-to-SARIF-Level Mapping](../user-stories/02-severity-to-sarif-level-mapping.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function | `sarifLevel(severity string) string` in `internal/report/sarif.go` |
| Test Framework | `go test` (table-driven, `t.Run` per case) | Follows `internal/report/render_test.go` convention |
| Key Dependencies | `github.com/samestrin/atcr/reconcile` (`reclib` alias) | `reclib.NormalizeSeverity`, `reclib.SeverityRank` — no new dependency |

### Related Files (from codebase-discovery.json)

- [`internal/report/sarif.go`](../../../../../internal/report/sarif.go) — create/modify: add `sarifLevel(severity string) string`, called from `renderSarif` (Story 1) when populating each `run.results[].level` field. Import `reclib "github.com/samestrin/atcr/reconcile"` following the alias convention already used in [`internal/report/render.go:11`](../../../../../internal/report/render.go).
- [`internal/report/sarif_test.go`](../../../../../internal/report/sarif_test.go) — create: table-driven `TestSarifLevel` covering all four canonical severities plus unrecognized/edge-case inputs, following [`internal/report/render_test.go`](../../../../../internal/report/render_test.go)'s `t.Run` subtest convention (e.g. [`TestRender_GoldenFiles` at line 74](../../../../../internal/report/render_test.go) and [`TestRender_MixedCaseSeverityGridBucketing` at line 401](../../../../../internal/report/render_test.go)).
- [`reconcile/severity.go`](../../../../../reconcile/severity.go) — reference only (no changes): `SeverityRank` ([`reconcile/severity.go:19-24`](../../../../../reconcile/severity.go)) and `NormalizeSeverity` ([`reconcile/severity.go:29`](../../../../../reconcile/severity.go)) are the sole rubric source this function must call — CRITICAL=4, HIGH=3, MEDIUM=2, LOW=1, unrecognized=absent/rank 0.
- [`internal/fanout/postprocess.go`](../../../../../internal/fanout/postprocess.go) — reference only (no changes): lines 28-40 are the cited precedent for correct reuse (`reclib.NormalizeSeverity` / `reclib.SeverityRank` called directly, no local redefinition).

### Technical References

- [SARIF 2.1.0 Schema Reference](../documentation/sarif-schema-reference.md)
- [GitHub Code Scanning SARIF Integration Constraints](../documentation/github-code-scanning-integration.md)

## Happy Path Scenarios

**Scenario 1: CRITICAL maps to error**
- **Given** a finding with `Severity` field `"CRITICAL"`
- **When** `sarifLevel("CRITICAL")` is called
- **Then** it returns `"error"`

**Scenario 2: HIGH maps to error**
- **Given** a finding with `Severity` field `"HIGH"`
- **When** `sarifLevel("HIGH")` is called
- **Then** it returns `"error"`

**Scenario 3: MEDIUM maps to warning**
- **Given** a finding with `Severity` field `"MEDIUM"`
- **When** `sarifLevel("MEDIUM")` is called
- **Then** it returns `"warning"`

**Scenario 4: LOW maps to note**
- **Given** a finding with `Severity` field `"LOW"`
- **When** `sarifLevel("LOW")` is called
- **Then** it returns `"note"`

**Scenario 5: renderSarif wires sarifLevel into every result**
- **Given** a slice of `reconcile.JSONFinding` with mixed severities
- **When** `renderSarif` builds the SARIF document
- **Then** each `run.results[].level` value equals `sarifLevel(finding.Severity)` for the corresponding finding, and no other string comparison against `Severity` exists in `sarif.go`

## Edge Cases

**Edge Case 1: Lowercase and mixed-case input normalizes to the canonical-case output**
- **Given** `sarifLevel("critical")`, `sarifLevel("High")`, `sarifLevel("mEdIuM")`
- **When** each is called
- **Then** each returns the same value as its canonical-case counterpart (`"error"`, `"error"`, `"warning"`), because the function normalizes via `reclib.NormalizeSeverity` before lookup

**Edge Case 2: Whitespace-padded input normalizes to the canonical-case output**
- **Given** `sarifLevel("  HIGH  ")`
- **When** called
- **Then** it returns `"error"` (leading/trailing whitespace trimmed by `reclib.NormalizeSeverity`)

**Edge Case 3: Empty string input**
- **Given** `sarifLevel("")`
- **When** called
- **Then** it returns the defined fallback level `"warning"` (not an empty string, not a panic)

**Edge Case 4: Unrecognized severity token**
- **Given** `sarifLevel("BOGUS")` or any string not present in `reclib.SeverityRank`
- **When** called
- **Then** it returns the defined fallback level `"warning"`

**Edge Case 5: No fourth SARIF level ever produced**
- **Given** every possible input string (canonical, mixed-case, empty, unrecognized)
- **When** `sarifLevel` is called
- **Then** the return value is always one of exactly `"error"`, `"warning"`, `"note"` — never `"none"` and never any other string, since GitHub Code Scanning does not recognize a `none` display level

## Error Conditions

This function has no HTTP surface and cannot error in the Go `error`-return sense; "error conditions" here means defined behavior for invalid/unrecognized input rather than an HTTP status.

**Error Scenario 1: Unrecognized or empty severity string**
- Condition: input severity does not normalize to a key present in `reclib.SeverityRank` (rank 0, per `SeverityRank`'s documented behavior for absent keys)
- Required behavior: function returns the fallback level `"warning"` — it must not return `""`, must not panic, and must not return an invalid SARIF level string
- No error value is returned or logged by `sarifLevel` itself (it is a pure string-to-string mapping); callers needing to flag unrecognized severities do so separately, outside this function's scope

## Performance Requirements
- **Response Time:** O(1) per call — a single map lookup plus a constant number of integer comparisons; no I/O, no allocation beyond the returned string constant.
- **Throughput:** Must not measurably affect `renderSarif`'s ability to process large finding sets (hundreds to low thousands of findings) within existing `atcr report` performance expectations; no benchmark regression on `internal/report` package tests.

## Security Considerations
- **Authentication/Authorization:** Not applicable — pure function, no I/O, no external state.
- **Input Validation:** Treats `severity` as an untrusted string (may originate from a third-party reviewer agent's JSON output). Must handle any input string (including empty, malformed, unexpected-length, or non-ASCII) without panicking. No use of the raw input in any format string, file path, or shell context — this function only compares and returns fixed string constants, so injection risk is inherently absent, but the implementation must not deviate from that shape (e.g. must not echo the raw input back in the returned value).

## Test Implementation Guidance
**Test Type:** UNIT

**Test Data Requirements:**
- All four canonical severity strings in canonical case: `"CRITICAL"`, `"HIGH"`, `"MEDIUM"`, `"LOW"`
- Case-variant forms: `"critical"`, `"High"`, `"mEdIuM"`, `"low"`
- Whitespace-padded forms: `"  HIGH  "`, `"\tLOW\n"`
- Invalid/edge forms: `""`, `"BOGUS"`, `"UNKNOWN"`
- No mocking of `reclib.NormalizeSeverity`/`reclib.SeverityRank` — call the real canonical rubric so drift between `sarifLevel` and the rubric is caught, not hidden by a stub

**Mock/Stub Requirements:** None. `sarifLevel` has no external dependencies to mock; tests call it directly with string literals per the table-driven pattern in `internal/report/render_test.go`.

## Definition of Done

**Auto-Verified:**
- [x] All tests passing (`go test ./internal/report/...`)
- [x] No linting errors (`golangci-lint` / project lint target)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `sarifLevel(severity string) string` exists in `internal/report/sarif.go` and is the only severity-comparison site in that file (verified by code inspection — no second CRITICAL/HIGH/MEDIUM/LOW string-comparison chain)
- [x] `sarifLevel` calls `reclib.NormalizeSeverity` before any comparison and derives its branches from `reclib.SeverityRank`, never a locally redefined map
- [x] Table-driven tests in `internal/report/sarif_test.go` cover all four canonical severities plus at least one unrecognized/empty-string edge case, using `t.Run` subtests
- [x] Every possible input maps to exactly one of `"error"` / `"warning"` / `"note"` — confirmed by an edge-case test asserting no `"none"` and no empty-string return

**Manual Review:**
- [ ] Code reviewed and approved

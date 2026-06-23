# Acceptance Criteria: Path-Validation Isolation and Schema Independence

**Related User Story:** [03: JSON Format Adapter (reconcile-json/v1)](../user-stories/03-json-format-adapter.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package — `encoding/json` adapter | stdlib-only in shipped code |
| Test Framework | `go test` + testify | co-located `*_test.go` |
| Key Dependencies | `reconcile.Finding`, `reconcile.Result` | library `Finding` does NOT carry path-validation fields |
| Schema Family | `reconcile-json/v1` | versioned INDEPENDENTLY of `atcr-findings/v1` |

### Related Files (from codebase-discovery.json)
- `reconcile/adapter/json/adapter.go` - create: encode/decode paths that map ONLY the library `Finding` fields; never reference `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`.
- `reconcile/adapter/json/adapter_test.go` - create: leakage test asserting none of the four path-validation field names appear in encoded bytes; version-string assertion test.
- `internal/reconcile/adapter/adapter.go` - read (ATCR-internal, NOT part of this AC's deliverable): the boundary adapter that DOES handle path-validation fields — confirms the separation of concerns the external adapter relies on.
- `reconcile/finding.go` (lifted in Story 2) - read: source of the pure library `Finding` type that excludes path-validation fields by construction.
- `docs/findings-format.md` - read: documents `atcr-findings/v1`; the adapter's schema is separate and versioned independently — the package doc and README must state this.

## Happy Path Scenarios
**Scenario 1: Encoded output contains zero path-validation field names**
- **Given** a `Result` whose findings were produced by the reconciler (which internally may have computed path-validation fields on the ATCR side via the boundary adapter)
- **When** the external JSON adapter encodes that `Result`
- **Then** the output bytes contain NONE of the strings `"PathValid"`, `"PathWarning"`, `"PathSuggestion"`, `"ClusterMerged"` (in any case, as JSON key or value) — verified by `bytes.Contains` checks for all four names

**Scenario 2: Version string is always reconcile-json/v1**
- **Given** any `Result` encoded through the adapter
- **When** the output is parsed
- **Then** the top-level `version` field equals exactly `"reconcile-json/v1"` — never `"atcr-findings/v1"`, never empty, never a different version

**Scenario 3: Decode rejects atcr-findings/v1 input**
- **Given** input carrying `"version":"atcr-findings/v1"`
- **When** the decode function is called
- **Then** it returns an error refusing the mismatched schema family, preventing silent cross-format ingestion

## Edge Cases
**Edge Case 1: Additive-only evolution within v1**
- **Given** a future producer that emits a NEW field inside `reconcile-json/v1` (e.g. `"sarif_rule"`)
- **When** the adapter decodes that input
- **Then** the new field is ignored (unknown-field tolerance from AC 03-01), and the version remains `reconcile-json/v1` — no breaking change inside the versioned family

**Edge Case 2: Producer emits path-validation fields in input**
- **Given** input that (incorrectly) includes `"PathValid":true` in a finding
- **When** decoded
- **Then** the field is dropped (the library `Finding` has no such field, so `encoding/json` ignores it by default); the resulting `[]Source` is clean

**Edge Case 3: Version field missing entirely**
- **Given** input with no `version` field
- **When** decoded
- **Then** the adapter returns an error requiring the `version` field (strict on the contract field, tolerant on extras — mirrors AC 03-01)

## Error Conditions
**Error Scenario 1: Path-validation field name leaks into output**
- Detected by test: `bytes.Contains(output, []byte("PathValid"))` (and the three siblings) returns `false`. If any returns `true`, the test fails with: `path-validation field leaked into external schema: <field_name>`
- HTTP status / error code: N/A (test failure surfaces the leakage)

**Error Scenario 2: Version string drift**
- Detected by test: parse output, assert `version == "reconcile-json/v1"`. If the assertion fails, the test fails with: `version string drifted: got <X>, want reconcile-json/v1`

**Error Scenario 3: Schema confusion — atcr-findings/v1 input accepted**
- Detected by test: feed `atcr-findings/v1` input, assert decode returns a non-nil error. If decode succeeds, the test fails with: `adapter accepted atcr-findings/v1 input, breaking schema independence`

## Performance Requirements
- **Response Time:** Leakage checks are test-time only; no runtime cost in shipped code (the library `Finding` type structurally excludes the fields).
- **Throughput:** The structural exclusion means no filtering pass at encode time — zero overhead.

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface.
- **Input Validation:** Strict on `version` (contract field), tolerant on extras. Path-validation fields in input are silently dropped, preventing accidental coupling to ATCR internals.
- **Schema Independence:** The `reconcile-json/v1` version string and field set are distinct from `atcr-findings/v1`. Embedders cannot assume compatibility between the two; the package doc and README must state this explicitly.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A `Result` fixture with fully-populated findings (to maximize the chance of leakage if any tag is wrong); an input fixture with `"version":"atcr-findings/v1"`; an input fixture with `"PathValid":true` injected.
**Mock/Stub Requirements:** None — uses the real lifted `Finding` type. Leakage check uses `bytes.Contains` on raw output bytes (case-sensitive JSON key names as documented); version check parses into a typed envelope struct.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./reconcile/adapter/json/...`)
- [ ] No linting errors (`go vet`, `golangci-lint`)
- [ ] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [ ] Encoded output contains none of `PathValid`, `PathWarning`, `PathSuggestion`, `ClusterMerged`
- [ ] Output `version` field is exactly `"reconcile-json/v1"` for every encoded `Result`
- [ ] Decode rejects input with `"version":"atcr-findings/v1"`
- [ ] Decode drops path-validation fields if a producer incorrectly emits them
- [ ] Package doc and README state the schema independence from `atcr-findings/v1` and the additive-only evolution policy

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Confirm the external adapter never imports or references the ATCR boundary adapter (`internal/reconcile/adapter/adapter.go`)

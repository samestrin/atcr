# Acceptance Criteria: Decode Single and Array Source Objects (reconcile-json/v1)

**Related User Story:** [03: JSON Format Adapter (reconcile-json/v1)](../user-stories/03-json-format-adapter.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package — `encoding/json` adapter | stdlib-only in shipped code |
| Test Framework | `go test` + testify | co-located `*_test.go` |
| Key Dependencies | `encoding/json`, `reconcile.Source`, `reconcile.Finding` (lifted in Story 2) | no third-party schema library |
| Schema Family | `reconcile-json/v1` | versioned independently of `atcr-findings/v1` |

## Related Files
- `reconcile/adapter/json/adapter.go` - create: decode entrypoint; sniffs first non-space byte (`[` vs `{`) and unmarshals into either `[]envelope` or a single `envelope` wrapped into a one-element slice, mapping each envelope to one `reconcile.Source`.
- `reconcile/adapter/json/adapter_test.go` - create: unit tests for single-object, array, empty-findings, unknown-field tolerance, and malformed-input rejection; round-trip fixtures live alongside.
- `reconcile/finding.go` (or equivalent lifted location from Story 2) - read: source of the `Finding` JSON struct tags that the envelope's `findings[]` map onto.
- `docs/findings-format.md` - read: conceptual reference for the wire format the adapter round-trips; `reconcile-json/v1` is its own independently-versioned schema.

## Happy Path Scenarios
**Scenario 1: Single source object decodes to a one-element []Source**
- **Given** an input JSON object `{"version":"reconcile-json/v1","source":"reviewer-a","findings":[{"severity":"high","file":"main.go","line":42,"problem":"nil deref","fix":"add nil check","category":"bug","est_minutes":15,"evidence":"...","reviewer":"reviewer-a"}]}`
- **When** the decode function is called with that input
- **Then** it returns `[]reconcile.Source` of length 1 whose `[0].Name == "reviewer-a"` and `[0].Findings[0].Severity == "high"` with all 9 wire fields populated from the library `Finding` JSON struct tags

**Scenario 2: Array of source objects decodes to N-element []Source**
- **Given** an input JSON array `[{"version":"reconcile-json/v1","source":"a","findings":[...]},{"version":"reconcile-json/v1","source":"b","findings":[...]}]`
- **When** the decode function is called with that input
- **Then** it returns `[]reconcile.Source` of length 2 preserving array order, with `[0].Name == "a"` and `[1].Name == "b"`

**Scenario 3: Unknown producer fields are ignored by default**
- **Given** an input object carrying extra keys the schema does not define (e.g. `"tool":"semgrep"`, `"rule_id":"G123"`)
- **When** the decode function is called
- **Then** decoding succeeds without error and the extra keys are dropped (no `DisallowUnknownFields`), matching ATCR's provider-client convention

## Edge Cases
**Edge Case 1: Source with empty findings array**
- **Given** `{"version":"reconcile-json/v1","source":"empty","findings":[]}`
- **When** decoded
- **Then** returns `[]Source` of length 1 with `Findings` being a non-nil empty slice (or nil per the library lift contract — assert the agreed shape)

**Edge Case 2: Leading whitespace before first byte**
- **Given** input prefixed with spaces, newlines, or a UTF-8 BOM
- **When** decoded
- **Then** the sniff skips non-space bytes correctly: `\n  [` is treated as an array; `\n  {` is treated as a single object

**Edge Case 3: Multiple findings in one source**
- **Given** a single source object with 3 findings
- **When** decoded
- **Then** `[0].Findings` has length 3 and each `Finding` preserves its field-to-tag mapping (including `reviewer` -> `Reviewer`)

## Error Conditions
**Error Scenario 1: Malformed JSON**
- Error message: `json: invalid character ...` (or wrapped adapter error)
- HTTP status / error code: N/A (library call returns `error`); caller decides mapping

**Error Scenario 2: Missing required `version` field**
- Given input without `"version"`, when decoded, then the adapter returns an error indicating `version` must be `"reconcile-json/v1"` (strict on the contract field, tolerant on extras)

**Error Scenario 3: Wrong schema version**
- Given `"version":"atcr-findings/v1"`, when decoded, then the adapter returns an error refusing the mismatched schema family

## Performance Requirements
- **Response Time:** Decoding 1,000 findings completes in < 50ms on commodity hardware (stdlib `encoding/json` baseline).
- **Throughput:** No allocations beyond what `encoding/json.Unmarshal` produces; the adapter itself adds no copies of the input bytes (operate on the caller's `[]byte`).

## Security Considerations
- **Authentication/Authorization:** N/A — library has no auth surface; embedders enforce auth at the network boundary.
- **Input Validation:** Reject mismatched `version`; tolerate unknown fields; cap input size at the caller's discretion (the adapter operates on bytes handed to it, no file/network I/O).
- **Resource Exhaustion:** Deeply nested JSON is bounded by `encoding/json`'s default recursion limit; no custom recursion in the adapter.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Fixtures for (a) single-object input, (b) array input, (c) empty-findings source, (d) input with unknown fields, (e) malformed input, (f) wrong version. Fixtures live in `reconcile/adapter/json/testdata/`.
**Mock/Stub Requirements:** None — decode is a pure function over `[]byte` returning `[]Source`; the library `Finding` type is the real lifted type from Story 2. If Story 2 has not landed, stub the `Finding` struct tags first and refactor when it lands.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./reconcile/adapter/json/...`)
- [ ] No linting errors (`go vet`, `golangci-lint`)
- [ ] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [ ] Single-object input yields `[]Source` of length 1 with correct field mapping
- [ ] Array input yields `[]Source` of length N preserving order
- [ ] Unknown fields are ignored (no `DisallowUnknownFields`)
- [ ] Wrong or missing `version` returns an error
- [ ] Malformed JSON returns an error without panic

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Package doc states the `reconcile-json/v1` schema and its independence from `atcr-findings/v1`

# Acceptance Criteria: Byte-Stability and omitempty on Optional Fields

**Related User Story:** [03: JSON Format Adapter (reconcile-json/v1)](../user-stories/03-json-format-adapter.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package — `encoding/json` adapter | stdlib-only in shipped code |
| Test Framework | `go test` + testify | co-located `*_test.go` |
| Key Dependencies | `encoding/json`, `reconcile.Finding`, `reconcile.Verification` | `omitempty` applied to `Disagreement` and `*Verification` on the lifted `Finding` type |
| Schema Family | `reconcile-json/v1` | field order fixed by struct declaration order (`encoding/json` behavior) |

## Related Files
- `reconcile/adapter/json/adapter.go` - create: encode path that produces byte-identical output for identical `Result` inputs; depends on the library `Finding` struct tags already applying `omitempty` to `Disagreement` and `*Verification`.
- `reconcile/adapter/json/adapter_test.go` - create: byte-stability test (encode the same `Result` twice, assert `bytes.Equal`); absent-field test (no `disagreement`/`verification` keys when zero).
- `reconcile/finding.go` (lifted in Story 2) - read/modify: source of the `Finding` struct tags; verify `json:"disagreement,omitempty"` and `json:"verification,omitempty"` are present (if missing, this AC blocks on Story 2).
- `docs/findings-format.md` - read: reference for the deterministic-output guarantee the reconciler already provides; the adapter inherits byte-stability from it.

## Happy Path Scenarios
**Scenario 1: Identical Result yields byte-identical output across runs**
- **Given** a fixed `reconcile.Result` and a fixed `Options{ReconciledAt: T}`
- **When** the encode function is called twice on the same input
- **Then** the two output `[]byte` slices are exactly equal (`bytes.Equal`), confirming determinism

**Scenario 2: Absent disagreement produces no key**
- **Given** a `Finding` whose `Disagreement` is the zero value
- **When** encoded
- **Then** the finding's JSON object contains NO `"disagreement"` key (verified by parsing and asserting the key is absent, AND by grepping the raw bytes)

**Scenario 3: Absent verification produces no key**
- **Given** a `Finding` whose `*Verification` is `nil`
- **When** encoded
- **Then** the finding's JSON object contains NO `"verification"` key

**Scenario 4: Present optional fields render fully**
- **Given** a `Finding` with a populated `Disagreement` and a populated `*Verification{Verdict, Skeptic, Notes}`
- **When** encoded
- **Then** both keys appear with their full sub-objects, and `verification` carries `verdict`, `skeptic`, and `notes` fields

## Edge Cases
**Edge Case 1: Partial Verification — only some sub-fields set**
- **Given** a `*Verification` with `Verdict` set but `Skeptic` and `Notes` empty
- **When** encoded
- **Then** the `verification` object renders with the set fields and omits the empty ones per each sub-field's own `omitempty` policy (assert the agreed contract)

**Edge Case 2: Empty Reviewers slice**
- **Given** a `Finding` with `Reviewers: []string{}` (or nil per lift contract)
- **When** encoded
- **Then** `"reviewers":[]` (or absent per the agreed `omitempty` policy — assert explicitly which)

**Edge Case 3: Field order is fixed across encodings**
- **Given** two `Result` values with findings in different in-memory allocation order but identical logical content
- **When** encoded
- **Then** the field order within each finding object is identical (struct declaration order), so the bytes match

## Error Conditions
**Error Scenario 1: omitempty missing on Disagreement**
- Detected by test: encode a Finding with zero `Disagreement` and assert the raw bytes do NOT contain `"disagreement"`. If the assertion fails, the test fails with: `disagreement key leaked into output; check Finding struct tag for omitempty`
- HTTP status / error code: N/A (test failure surfaces the missing tag)

**Error Scenario 2: omitempty missing on *Verification**
- Detected by test: encode a Finding with `nil` `*Verification` and assert the raw bytes do NOT contain `"verification"`. If the assertion fails, the test fails with: `verification key leaked into output; check Finding struct tag for omitempty`

**Error Scenario 3: Field order changes between encodings**
- Detected by byte-stability test failing (`bytes.Equal` returns false on identical input)

## Performance Requirements
- **Response Time:** Byte-stability adds zero runtime cost — it relies on `encoding/json`'s deterministic struct-field ordering and `omitempty` tags, not on post-processing.
- **Throughput:** No sorting or re-marshaling pass; output is single-pass.

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface.
- **Input Validation:** Byte-stability is a contract guarantee, not an input check; it protects embedders that hash or diff the output (e.g. for caching or audit) from spurious changes.
- **Reproducibility:** Deterministic output is what makes the reconciler a credible reference implementation; breaking it undermines the architectural moat.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Two `Result` fixtures — one with both optional fields populated, one with both absent. A fixed `Options.ReconciledAt` to remove timestamp variance.
**Mock/Stub Requirements:** None — encode is a pure function. Use `bytes.Equal` for stability; use `encoding/json.Unmarshal` into `map[string]json.RawMessage` to assert key presence/absence without coupling to field order.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./reconcile/adapter/json/...`)
- [ ] No linting errors (`go vet`, `golangci-lint`)
- [ ] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [ ] Same `Result` + fixed `ReconciledAt` encodes to byte-identical output across two calls
- [ ] Absent `Disagreement` produces no `"disagreement"` key in output bytes
- [ ] `nil` `*Verification` produces no `"verification"` key in output bytes
- [ ] Populated `*Verification` renders `verdict`, `skeptic`, `notes` sub-fields
- [ ] Field order within a finding object is stable across encodings

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Confirm `Finding` struct tags carry `omitempty` on `Disagreement` and `*Verification` (coordinate with Story 2 if not yet present)

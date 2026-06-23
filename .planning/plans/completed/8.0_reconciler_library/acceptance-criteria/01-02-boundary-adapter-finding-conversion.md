# Acceptance Criteria: Boundary Adapter Finding Conversion and Path-Validation Stamping

**Related User Story:** [01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change](../user-stories/01-reference-implementation-preservation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package (`internal/reconcile/adapter`) | New boundary package isolating conversion + I/O |
| Test Framework | go test + testify | New RED tests for the conversion (the only new tests in this story) |
| Key Dependencies | `github.com/samestrin/atcr/reconcile`, `internal/stream`, `internal/reconcile` (gate.go, validate.go stay) | Adapter bridges library and ATCR internals |

### Related Files (from codebase-discovery.json)
- `internal/reconcile/adapter/adapter.go` - create: `stream.Finding` → `reconcile.Finding` conversion, `Result` → `JSONFinding` wrapping, path-validation field stamping, file I/O relocated from `emit.go`/`discover.go`
- `internal/reconcile/emit.go` - modify: split public types out to library; retain `JSONFinding` ATCR-internal wrapper with `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` fields; I/O moves to adapter (public types originally at `internal/reconcile/emit.go:40`/`emit.go:74`, verdict constants at `emit.go:61-63`)
- `internal/reconcile/discover.go` - modify: split `Source` type out to library; `findings.txt` discovery I/O moves to adapter (`Source` originally at `internal/reconcile/discover.go:25`)
- `internal/reconcile/validate.go` - modify: stays ATCR-internal, imports library `Verification` + `Verdict` constants unchanged; stamps `PathValid`/`PathWarning`/`PathSuggestion` via the adapter (`validateFindingPaths` at `internal/reconcile/validate.go:21`)
- `internal/reconcile/adapter/adapter_test.go` - create: RED tests for `stream.Finding` ↔ `reconcile.Finding` conversion and path-validation stamping

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — defines the `Verification` struct fields (`Skeptic`, `Verdict`, `Notes`) and verdict enum that must preserve pointer identity across the adapter boundary.

## Happy Path Scenarios
**Scenario 1: stream.Finding converts to reconcile.Finding before Reconcile call**
- **Given** a caller (e.g. `cmd/atcr/reconcile.go:35` `runReconcile`) has a `[]stream.Finding` and constructs `[]reconcile.Source`
- **When** the adapter converts each `stream.Finding` to a `reconcile.Finding` (9 core wire fields: Severity, File, Line, Problem, Fix, Category, EstMinutes, Evidence, Reviewer/Reviewers, Confidence) and calls `reconcile.Reconcile(sources, opts)`
- **Then** the library returns a `reconcile.Result` with `Merged` findings carrying the correct `*Verification` pointers, and all 9 wire fields round-trip with zero data loss

**Scenario 2: Library Result wraps back into ATCR-internal JSONFinding with path-validation fields**
- **Given** the library returns a `reconcile.Result` containing merged `Finding` values with `*Verification` pointers
- **When** the adapter wraps each merged `Finding` back into the ATCR-internal `JSONFinding` (`emit.go:74`) and runs `validateFindingPaths` (`validate.go:21`)
- **Then** `PathValid`, `PathWarning`, `PathSuggestion`, and `ClusterMerged` are stamped onto the `JSONFinding` at the adapter boundary, and the `Verification` pointer is shared (pointer-identity preserved) so `gate.go` and `internal/debate` can still read/mutate it

**Scenario 3: Path-validation fields are ATCR-only and absent from the library Finding**
- **Given** the library's `Finding` struct carries only the 9 core wire fields + `Disagreement` + `*Verification`
- **When** the adapter constructs the `JSONFinding`
- **Then** `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` exist ONLY on `JSONFinding` (not on `reconcile.Finding`), preserving the stdlib-only library boundary

## Edge Cases
**Edge Case 1: Finding with empty Evidence or nil Verification**
- **Given** a `stream.Finding` with an empty `Evidence` slice and a nil `*Verification`
- **When** the adapter converts it to `reconcile.Finding`
- **Then** the conversion produces a `reconcile.Finding` with an empty `Evidence` and nil `*Verification` (zero-value passthrough, no panic)

**Edge Case 2: Reviewers field with single vs. multi-reviewer**
- **Given** a `stream.Finding` with `Reviewer` (single) and another with `Reviewers` (multi)
- **When** the adapter converts both
- **Then** both map to the library `Finding`'s reviewer field(s) without truncation or duplication

**Edge Case 3: Disagreement field on the library Finding**
- **Given** a library `Finding` carries a non-nil `Disagreement` (set by `BuildDisagreements`)
- **When** the adapter wraps it into `JSONFinding`
- **Then** the `Disagreement` is carried through to the `JSONFinding` unchanged (it is part of the library `Finding`, not an ATCR-only field)

## Error Conditions
**Error Scenario 1: Conversion drops a wire field**
- Error message: RED test `TestStreamFindingToReconcileFinding_RoundTrip` fails: field mismatch on `<FieldName>`
- HTTP status / error code: go test exit code 1

**Error Scenario 2: Path-validation stamping mutates the library Finding**
- Error message: RED test `TestPathValidationStampsOnlyJSONFinding` fails: `PathValid` found on `reconcile.Finding` (must be ATCR-only)
- HTTP status / error code: go test exit code 1

**Error Scenario 3: Verification pointer identity broken across the boundary**
- Error message: RED test `TestVerificationPointerIdentityPreserved` fails: `gate.go` and `internal/debate` see different `*Verification` instances after the adapter wraps the result
- HTTP status / error code: go test exit code 1

## Performance Requirements
- **Response Time:** The adapter conversion is O(n) in the number of findings; per-finding conversion must complete in < 1µs (struct copy + slice aliasing, no allocation beyond the `JSONFinding` wrapper). The full `Reconcile` call's latency must not regress against the pre-extraction baseline.
- **Throughput:** Conversion must handle the largest real fixture corpus (thousands of findings) without measurable overhead added by the adapter layer.

## Security Considerations
- **Authentication/Authorization:** N/A (internal conversion, no network/auth boundary)
- **Input Validation:** The adapter must not silently coerce or normalize fields during conversion — it is a 1:1 field copy. Any validation (path validation) is performed by `validate.go` AFTER conversion, not during it, preserving separation of concerns.

## Test Implementation Guidance
**Test Type:** UNIT (new RED tests, the only new tests in this story)
**Test Data Requirements:** A representative `stream.Finding` fixture covering all 9 wire fields, single and multi-reviewer variants, nil and non-nil `*Verification`, empty and non-empty `Evidence`, and a `Disagreement`-bearing finding.
**Mock/Stub Requirements:** None — the conversion is a pure struct-mapping function; test it with real `stream.Finding` and `reconcile.Finding` values. Assert field-by-field equality and `*Verification` pointer identity (compare addresses, not just values).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (including new RED tests for the conversion)
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `internal/reconcile/adapter/adapter.go` converts `stream.Finding` → `reconcile.Finding` (9 wire fields round-trip, zero data loss)
- [ ] Adapter wraps `reconcile.Result` back into `JSONFinding` and stamps `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged`
- [ ] `*Verification` pointer identity preserved across the boundary (verified by address comparison in a RED test)
- [ ] Path-validation fields exist ONLY on `JSONFinding`, never on `reconcile.Finding`

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Confirm file I/O from `emit.go`/`discover.go` is relocated into the adapter, not duplicated

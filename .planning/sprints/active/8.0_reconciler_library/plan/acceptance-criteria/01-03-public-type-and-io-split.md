# Acceptance Criteria: Public Type and File I/O Split (emit.go / discover.go)

**Related User Story:** [01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change](../user-stories/01-reference-implementation-preservation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go package split (library types move; I/O stays in ATCR adapter) | Mechanical split of entangled files |
| Test Framework | go test + testify | Existing corpus validates behavior is unchanged |
| Key Dependencies | `github.com/samestrin/atcr/reconcile` (types), `internal/reconcile/adapter` (I/O) | No copy of `Verification` — it becomes library API |

### Related Files (from codebase-discovery.json)
- `reconcile/emit.go` - create: public types `Verification` (moved from `internal/reconcile/emit.go:40`), `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` constants (`internal/reconcile/emit.go:61-63`) move to library
- `reconcile/discover.go` - create: `Source` type (moved from `internal/reconcile/discover.go:25`) moves to library
- `internal/reconcile/emit.go` - modify: remove moved public types; retain `JSONFinding` ATCR-internal wrapper (`internal/reconcile/emit.go:74`); file I/O (read/write `findings.json`) moves to `internal/reconcile/adapter`
- `internal/reconcile/discover.go` - modify: remove moved `Source` type; `findings.txt` discovery I/O moves to `internal/reconcile/adapter`
- `internal/reconcile/adapter/adapter.go` - modify: receive relocated file I/O (read/write findings, discovery) that previously lived in `emit.go`/`discover.go`

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — source of truth for the `Verification` struct and verdict constants that move to the library as public API.

## Happy Path Scenarios
**Scenario 1: Verification type becomes public library API unchanged**
- **Given** `internal/reconcile/emit.go:40` currently defines `Verification` (used by `Merged.Verification`, `gate.go`, and `internal/debate`)
- **When** `Verification` is moved to `reconcile/emit.go` as exported library API (no copy, no reshape)
- **Then** `internal/reconcile/gate.go` and `internal/debate/emit.go:107` (`applyRulings`) import `reconcile.Verification` and operate on the same `*Verification` pointer, preserving pointer-identity semantics

**Scenario 2: Verdict constants move to library as exported consts**
- **Given** `internal/reconcile/emit.go:61-63` defines `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` constants
- **When** these constants move to `reconcile/emit.go` as exported `const` declarations
- **Then** all consumers import `reconcile.VerdictConfirmed` etc. and no consumer re-declares these constants locally

**Scenario 3: Source type moves to library; findings.txt I/O stays in adapter**
- **Given** `internal/reconcile/discover.go:25` defines `Source` and the same file contains `findings.txt` discovery I/O
- **When** `Source` moves to `reconcile/discover.go` (pure type) and the discovery I/O moves to `internal/reconcile/adapter/adapter.go`
- **Then** the library's `Source` has no file-I/O dependency, and the adapter performs discovery by constructing `[]reconcile.Source` from `findings.txt`

**Scenario 4: File I/O (read/write findings.json) relocated to adapter**
- **Given** `internal/reconcile/emit.go` currently mixes `JSONFinding`/`Verification` type definitions with file I/O (read/write `findings.json`)
- **When** the I/O functions move to `internal/reconcile/adapter/adapter.go`
- **Then** the library package (`reconcile/`) contains zero file-I/O code (no `os`/`io` imports in non-test files), and all `findings.json` read/write happens through the adapter

## Edge Cases
**Edge Case 1: JSONFinding wraps library Finding, not copies it**
- **Given** `JSONFinding` (`emit.go:74`) embeds or references the library `Finding`
- **When** the split is performed
- **Then** `JSONFinding` embeds `reconcile.Finding` (or holds it by value) so the 9 wire fields are not duplicated; ATCR-only fields (`PathValid` etc.) are added on top

**Edge Case 2: Constants used in const-expressions by consumers**
- **Given** a consumer uses a `Verdict*` constant in a `const` block or `switch` case
- **When** the constant moves to the library
- **Then** the consumer's `const`/`switch` still compiles because the library constant is an untyped/exported `const` (not a `var`)

## Error Conditions
**Error Scenario 1: I/O accidentally moves into the library**
- Error message: `go list -deps ./reconcile/...` shows `os` or `io/ioutil` in a non-test dependency (violates stdlib-only-pure mandate)
- HTTP status / error code: CI guard test fails, exit code 1

**Error Scenario 2: Verification copied instead of moved (two definitions)**
- Error message: `redeclared` or duplicate-symbol compile error, or a RED test fails because `*Verification` pointers from the two types are incompatible
- HTTP status / error code: go build exit code 1

**Error Scenario 3: Consumer still references internal/reconcile for a moved type**
- Error message: `cannot find package internal/reconcile.Verification` (moved out) or `imported and not used`
- HTTP status / error code: go build exit code 1

## Performance Requirements
- **Response Time:** The split is compile-time only; no runtime overhead. Moving types across packages does not change struct layout or allocation.
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** The split must not alter the `Verification` struct's fields or the `Verdict` constant values — they are wire-format-critical. Any change would break fixture byte-identity (covered in AC 01-05).

## Test Implementation Guidance
**Test Type:** INTEGRATION (existing corpus validates; no new RED tests for the split itself)
**Test Data Requirements:** The existing `internal/reconcile/emit_test.go:20` (`TestReconcile_TwoReviewersAgreeHighConfidence`) corpus exercises the public types end-to-end through the adapter.
**Mock/Stub Requirements:** None. Verification is mechanical: compile in both packages, run the existing corpus, diff fixtures. A guard test asserting `reconcile/` has no `os`/`io` imports in non-test files may be added.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (existing corpus green)
- [x] No linting errors
- [x] Build succeeds in both root and `./reconcile/`

**Story-Specific:**
- [x] `Verification`, `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable`, `Source`, and the library `Finding` move to `reconcile/` (no copies)
- [x] `emit.go`/`discover.go` file I/O relocates to `internal/reconcile/adapter/adapter.go`; library has zero `os`/`io` imports in non-test files
- [x] `Merged.Verification` pointer identity preserved (`gate.go` and `internal/debate` operate on the same `*Verification`)

**Manual Review:**
- [x] Code reviewed and approved
- [x] Confirm the split is mechanical (no field renames, no type reshaping)

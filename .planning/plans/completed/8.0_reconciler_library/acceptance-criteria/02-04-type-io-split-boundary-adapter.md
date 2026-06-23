# Acceptance Criteria: Type/I/O Split and Boundary Adapter

**Related User Story:** [02: Embeddable Public API Module Scaffold](../user-stories/02-public-api-embeddability.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Type Extraction | Go type split | `emit.go`/`discover.go` types separated from their file I/O counterparts |
| Boundary Adapter | Go package (`internal/reconcile/adapter`) | Converts `stream.Finding` <-> `reconcile.Finding`, retains path-validation fields |
| Path Validation | ATCR-internal (`gate.go`, `validate.go`) | `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` stay behind adapter |
| JSON Wrapping | `encoding/json` | ATCR's `JSONFinding` wraps library `Finding` + path-validation fields |

### Related Files (from codebase-discovery.json)
- `reconcile/emit.go` - create: `Verification` struct (`internal/reconcile/emit.go:40`), `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` constants (`internal/reconcile/emit.go:61-63`), library `Finding` type — file I/O stays ATCR-internal
- `reconcile/discover.go` - create: `Source` type (`internal/reconcile/discover.go:25`) — `Discover()` file reading stays ATCR-internal
- `internal/reconcile/adapter/adapter.go` - create: `stream.Finding` <-> `reconcile.Finding` conversion, path-validation stamping, file I/O delegation
- `internal/reconcile/gate.go` - modify: imports library's `Verification` + `Verdict` constants unchanged (`IsFailing`/`CountAtOrAbove`, `internal/reconcile/gate.go:96`)

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — defines the `Verification` struct and verdict constants that move to the library as public API.

## Happy Path Scenarios
**Scenario 1: emit.go types move to library, I/O stays internal**
- **Given** `emit.go` currently mixes `Verification`/`Finding` types with file I/O functions
- **When** the split is performed
- **Then** `Verification`, `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable`, and the library `Finding` type live in `reconcile/emit.go`; the file I/O emit layer remains ATCR-internal

**Scenario 2: discover.go Source type moves, Discover() stays internal**
- **Given** `discover.go` currently mixes the `Source` type with `Discover()` file reading
- **When** the split is performed
- **Then** the `Source` type lives in `reconcile/discover.go`; `Discover()` file reading stays ATCR-internal behind the adapter

**Scenario 3: Adapter converts stream.Finding to reconcile.Finding**
- **Given** ATCR's fan-out engine produces `stream.Finding` values
- **When** the adapter at `internal/reconcile/adapter/adapter.go` converts them
- **Then** a `reconcile.Finding` is produced carrying the 9 wire-format fields, ready for `reconcile.Reconcile()`

**Scenario 4: gate.go imports verdict constants from library unchanged**
- **Given** `gate.go` uses `VerdictConfirmed`/`VerdictRefuted`/`VerdictUnverifiable` for `IsFailing`/`CountAtOrAbove`
- **When** the constants move to the library
- **Then** `gate.go` imports them from `github.com/samestrin/atcr/reconcile` with no visibility change (constants were already exported)

**Scenario 5: JSONFinding wraps library Finding + path-validation fields**
- **Given** ATCR's `JSONFinding` needs both wire-format fields and path-validation metadata
- **When** the adapter constructs a `JSONFinding`
- **Then** it embeds/wraps `reconcile.Finding` and adds `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` fields at the adapter boundary

## Edge Cases
**Edge Case 1: Path-validation fields are NOT in the library Finding**
- **Given** the library `Finding` type is defined with the 9 wire-format fields
- **When** a caller inspects the library `Finding`
- **Then** `PathValid`, `PathWarning`, `PathSuggestion`, `ClusterMerged` are absent — they exist only in ATCR's `JSONFinding` wrapper

**Edge Case 2: Adapter handles empty Findings slice**
- **Given** the adapter receives an empty `[]stream.Finding`
- **When** it converts to `[]reconcile.Finding`
- **Then** an empty slice is returned without error

**Edge Case 3: Adapter preserves all 9 wire-format fields during conversion**
- **Given** a `stream.Finding` with all fields populated
- **When** the adapter converts it to `reconcile.Finding`
- **Then** `Severity`, `File`, `Line`, `Problem`, `Fix`, `Category`, `EstMinutes`, `Evidence`, `Reviewer`/`Reviewers`, `Confidence` are all preserved

## Error Conditions
**Error Scenario 1: gate.go fails to find verdict constants**
- Error condition: `gate.go` still imports from old `internal/reconcile` path after constants moved
- Symptom: `go build` fails with `undefined: VerdictConfirmed` or similar
- Fix: update import to `github.com/samestrin/atcr/reconcile`

**Error Scenario 2: Adapter references path-validation fields on library Finding**
- Error condition: adapter code tries to set `PathValid` on a `reconcile.Finding`
- Symptom: compile error — `PathValid` is not a field of `reconcile.Finding`
- Fix: set path-validation fields on the `JSONFinding` wrapper, not on the library `Finding`

**Error Scenario 3: File I/O function accidentally moves to library**
- Error condition: `Discover()` or emit file-writing functions are placed in `reconcile/` instead of `internal/reconcile/adapter/`
- Symptom: library imports `os` or `bufio` — violates stdlib-only constraint (os/bufio are stdlib but file I/O is an ATCR concern, not a library concern)
- Fix: move file I/O functions to `internal/reconcile/adapter/adapter.go`

## Performance Requirements
- **Conversion Overhead:** `stream.Finding` -> `reconcile.Finding` conversion is O(1) per finding (field copy, no allocation beyond the struct)
- **No Reflection:** Adapter uses direct field assignment, not reflection, for conversion performance

## Security Considerations
- **Path Validation Boundary:** Path validation (`PathValid`/`PathWarning`/`PathSuggestion`) stays ATCR-internal — the library never touches filesystem paths, reducing its attack surface
- **File I/O Isolation:** All file reading/writing is delegated to the adapter layer, which can enforce ATCR's path-validation gate before passing data to the library

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Sample `stream.Finding` values with all 9 fields populated; sample `reconcile.Finding` values for reverse conversion
**Mock/Stub Requirements:** File I/O in the adapter can be tested with `t.TempDir()` fixtures; no mocking of the library itself needed
**Verification Commands:**
- `go build ./internal/reconcile/adapter/...` — adapter compiles
- `go test ./internal/reconcile/adapter/...` — adapter conversion tests pass
- `go vet ./internal/reconcile/...` — no import cycle warnings

## Definition of Done
**Auto-Verified:**
- [ ] `go build ./reconcile/...` exits 0 (library types compile)
- [ ] `go build ./internal/reconcile/adapter/...` exits 0 (adapter compiles)
- [ ] `go build ./...` exits 0 (full repo builds with import flips)
- [ ] No linting errors

**Story-Specific:**
- [ ] `reconcile/emit.go` contains `Verification`, verdict constants, and library `Finding` type — no file I/O functions
- [ ] `reconcile/discover.go` contains `Source` type — no `Discover()` function
- [ ] `internal/reconcile/adapter/adapter.go` performs `stream.Finding` <-> `reconcile.Finding` conversion
- [ ] `internal/reconcile/gate.go` imports verdict constants from `github.com/samestrin/atcr/reconcile`
- [ ] `PathValid`/`PathWarning`/`PathSuggestion`/`ClusterMerged` appear only in ATCR's `JSONFinding`, not in library `Finding`

**Manual Review:**
- [ ] Code reviewed and approved

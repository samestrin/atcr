# Acceptance Criteria: Root Replace Directive and Library Module Consumption

**Related User Story:** [01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change](../user-stories/01-reference-implementation-preservation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go module (nested) + root go.mod directive | `./reconcile/` has its own `go.mod`; root adds `replace` |
| Test Framework | go test + testify | testify confined to `*_test.go` |
| Key Dependencies | `github.com/samestrin/atcr/reconcile` (stdlib-only) | No third-party deps in non-test library files |

### Related Files (from codebase-discovery.json)
- `go.mod` - modify: add `require github.com/samestrin/atcr/reconcile` and `replace github.com/samestrin/atcr/reconcile => ./reconcile`
- `reconcile/go.mod` - create: nested module declaration `module github.com/samestrin/atcr/reconcile`, `go 1.25`, stdlib-only
- `reconcile/doc.go` - create: package doc string for the library package
- `cmd/atcr/reconcile.go` - modify: import `github.com/samestrin/atcr/reconcile` instead of `internal/reconcile` (boundary site at `cmd/atcr/reconcile.go:35`)

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — verification contract consumed by the library's public `Verification` type referenced in this AC's public API surface.

## Happy Path Scenarios
**Scenario 1: Root module resolves library via replace directive**
- **Given** the nested module `./reconcile/go.mod` exists with `module github.com/samestrin/atcr/reconcile` and the public API surface (`Reconcile`, `Source`, `Merged`, `Options`, `Result`, `Summary`, `Verification`, `Verdict` constants, `Finding`, `NormalizeSeverity`, `SeverityRank`) is present
- **When** the root `go.mod` adds `replace github.com/samestrin/atcr/reconcile => ./reconcile` and a consumer imports `github.com/samestrin/atcr/reconcile`
- **Then** `go build ./...` succeeds in the root module and resolves the import to the local `./reconcile/` directory

**Scenario 2: Library module is stdlib-only**
- **Given** the library's non-test source files are moved into `./reconcile/`
- **When** `go list -deps ./reconcile/...` is run from the library directory
- **Then** the dependency graph contains only standard-library packages (no `internal/stream`, no `internal/reconcile`, no third-party packages); testify appears only under `*_test.go` import paths

## Edge Cases
**Edge Case 1: go.mod version mismatch between root and nested module**
- **Given** the root `go.mod` declares `go 1.22` and the nested `reconcile/go.mod` declares a different `go` directive
- **When** `go build ./...` runs in the root
- **Then** the build either succeeds (compatible versions) or fails with a clear `go.mod` version error; the nested module's `go` directive is set to match the root's minimum

**Edge Case 2: Replace directive points at a missing directory**
- **Given** the root `go.mod` has the `replace` directive but `./reconcile/` does not exist or lacks a `go.mod`
- **When** `go build ./...` runs
- **Then** the build fails with a `replacement directory ... does not exist or contains no go.mod` error (caught before merge)

## Error Conditions
**Error Scenario 1: Consumer imports a path not exported by the library**
- Error message: `cannot find module providing package github.com/samestrin/atcr/reconcile/<missing>`
- HTTP status / error code: go build exit code 1

**Error Scenario 2: Library module accidentally imports ATCR internals**
- Error message: `use of internal package github.com/samestrin/atcr/internal/... not allowed`
- HTTP status / error code: go build exit code 1 (the `internal/` visibility boundary blocks the import)

## Performance Requirements
- **Response Time:** `go build ./...` cold-cache build time must not regress by more than 5% against the pre-extraction baseline (the nested module adds negligible overhead)
- **Throughput:** N/A (compile-time concern, not runtime)

## Security Considerations
- **Authentication/Authorization:** N/A (build-time module resolution)
- **Input Validation:** The `replace` directive pins the library to the local filesystem path `./reconcile/`, preventing accidental fetch of a remote (potentially untrusted) module of the same name. No `go get` of a remote `github.com/samestrin/atcr/reconcile` occurs.

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** A root `go.mod` with the `replace` directive and a minimal `./reconcile/` module containing the public API surface.
**Mock/Stub Requirements:** None — this is a real build resolution test. Run `go build ./...` and assert exit code 0; run `go list -deps ./reconcile/...` and assert no `internal/` or third-party paths in non-test deps. A guard test (`TestLibraryIsStdlibOnly`) may parse `go list -deps` JSON output.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors (`golangci-lint run` clean in root and `./reconcile/`)
- [x] Build succeeds (`go build ./...` exit 0 in root; `go build ./...` exit 0 in `./reconcile/`)

**Story-Specific:**
- [x] Root `go.mod` contains `replace github.com/samestrin/atcr/reconcile => ./reconcile`
- [x] `./reconcile/go.mod` declares `module github.com/samestrin/atcr/reconcile` with stdlib-only non-test deps
- [x] `go list -deps ./reconcile/...` (excluding `*_test.go`) shows zero `internal/` or third-party packages

**Manual Review:**
- [x] Code reviewed and approved
- [x] `replace` directive path confirmed to be `./reconcile` (not a remote URL)

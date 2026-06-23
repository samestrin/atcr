# Acceptance Criteria: Nested Module Scaffold with Root Replace Directive

**Related User Story:** [02: Embeddable Public API Module Scaffold](../user-stories/02-public-api-embeddability.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Module Definition | Go module (`go.mod`) | `module github.com/samestrin/atcr/reconcile`, `go 1.25`, no `require` block |
| Root Wiring | Go `replace` directive | `replace github.com/samestrin/atcr/reconcile => ./reconcile` in root `go.mod` |
| Build Verification | `go build ./reconcile/...` | Must compile with stdlib-only deps |
| Test Framework | `go test` + testify | testify allowed only in `*_test.go` |
| Linting | `golangci-lint`, `gofmt` | Both must be clean for the new module path |

## Related Files
- `reconcile/go.mod` - create: declares `module github.com/samestrin/atcr/reconcile`, `go 1.25`, empty `require` block
- `go.mod` - modify: add `replace github.com/samestrin/atcr/reconcile => ./reconcile` directive
- `reconcile/reconcile.go` - create: `Reconcile` entry point moved from `internal/reconcile/reconcile.go`
- `reconcile/merge.go` - create: `Merged` struct and merge logic moved from `internal/reconcile/merge.go`

## Happy Path Scenarios
**Scenario 1: Nested module builds standalone**
- **Given** a Go 1.25 toolchain is available and the repository is checked out at the repo root
- **When** `go build ./reconcile/...` is executed from the repository root
- **Then** the command exits with status 0 and produces no output, confirming the nested module compiles independently

**Scenario 2: Root replace directive resolves the module**
- **Given** the root `go.mod` contains `replace github.com/samestrin/atcr/reconcile => ./reconcile`
- **When** `go build ./...` is executed from the repository root
- **Then** both ATCR and the nested reconcile module compile successfully with zero errors

**Scenario 3: Nested module tests pass**
- **Given** test files exist in `reconcile/*_test.go` using testify
- **When** `go test ./reconcile/...` is executed
- **Then** all tests pass with exit status 0

## Edge Cases
**Edge Case 1: go mod tidy yields empty require block**
- **Given** the `reconcile/go.mod` file with `go 1.25` and no third-party imports in non-test files
- **When** `go mod tidy` is run inside the `./reconcile/` directory
- **Then** the `require` block remains empty or absent, confirming stdlib-only purity

**Edge Case 2: gofmt produces no diff**
- **Given** all `.go` files under `./reconcile/` are written
- **When** `gofmt -l ./reconcile` is executed
- **Then** no files are listed (all already formatted)

## Error Conditions
**Error Scenario 1: Missing replace directive**
- Error condition: root `go.mod` lacks the `replace` directive
- Symptom: `go build ./...` fails with `missing go.sum entry` or `cannot find module github.com/samestrin/atcr/reconcile`
- Fix: add `replace github.com/samestrin/atcr/reconcile => ./reconcile` to root `go.mod`

**Error Scenario 2: Third-party dependency leaked into library**
- Error condition: a non-test `.go` file under `./reconcile/` imports a non-stdlib package
- Symptom: `go mod tidy` in `./reconcile/` populates the `require` block
- Fix: remove the third-party import or move the code to a `*_test.go` file if testify is the dep

## Performance Requirements
- **Build Time:** `go build ./reconcile/...` completes in under 5 seconds (stdlib-only, no external fetch)
- **Module Resolution:** `go mod tidy` in `./reconcile/` completes without network access (no deps to resolve)

## Security Considerations
- **Dependency Audit:** The empty `require` block guarantees no unvetted third-party code enters the library
- **Module Path Integrity:** The `replace` directive is development-only; separate-repo publication (deferred) removes it and pins via go.sum

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Existing test corpus from `internal/reconcile/*_test.go` moved into `reconcile/*_test.go`
**Mock/Stub Requirements:** None — the reconciler is pure and stateless; tests use real `Source`/`Finding` fixtures
**Verification Commands:**
- `go build ./reconcile/...`
- `go test ./reconcile/...`
- `gofmt -l ./reconcile`
- `golangci-lint run ./reconcile/...`
- `cd reconcile && go mod tidy && grep require go.mod` (should show no require block)

## Definition of Done
**Auto-Verified:**
- [ ] `go build ./reconcile/...` exits 0
- [ ] `go test ./reconcile/...` passes
- [ ] `gofmt -l ./reconcile` produces no output
- [ ] `golangci-lint run ./reconcile/...` is clean

**Story-Specific:**
- [ ] `reconcile/go.mod` declares `module github.com/samestrin/atcr/reconcile` with `go 1.25` and no `require` block
- [ ] Root `go.mod` contains `replace github.com/samestrin/atcr/reconcile => ./reconcile`
- [ ] `go build ./...` from repo root succeeds (both modules compile)
- [ ] `go mod tidy` inside `./reconcile/` leaves the `require` block empty

**Manual Review:**
- [ ] Code reviewed and approved

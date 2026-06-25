# Acceptance Criteria: Stdlib-Only Boundary Enforcement

**Related User Story:** [02: Embeddable Public API Module Scaffold](../user-stories/02-public-api-embeddability.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Dependency Control | Go modules (`go.mod`) | Empty `require` block enforces stdlib-only |
| Import Auditing | `golangci-lint` | Custom linter config flags non-stdlib imports in non-test files |
| Test Isolation | Go build tags / file naming | testify imports confined to `*_test.go` files only |
| Import Scanner | `go list` / `goimports` | Programmatic verification of import paths |

### Related Files (from codebase-discovery.json)
- `reconcile/go.mod` - create: must have no `require` block (or empty) after `go mod tidy`
- `reconcile/dedupe.go` - create: token-set Jaccard dedupe using only `sort`, `strings` (no third-party deps) (`internal/reconcile/dedupe.go:53`)
- `reconcile/disagree.go` - create: `BuildDisagreements` using only stdlib (`internal/reconcile/disagree.go:102`)
- `reconcile/confidence.go` - create: `ConfidenceForVerdict`/`ConfidenceAtOrAbove` using only stdlib (`internal/reconcile/confidence.go:22`)
- `reconcile/severity.go` - create: `NormalizeSeverity`/`SeverityRank` moved from `internal/stream/severity.go:33`

## Happy Path Scenarios
**Scenario 1: Non-test files import only stdlib packages**
- **Given** all `.go` files under `./reconcile/` excluding `*_test.go` are written
- **When** the import statements are audited via `go list -deps ./reconcile/...` or grep
- **Then** every non-test file imports only `sort`, `strings`, `encoding/json`, `time`, or other stdlib packages — no third-party or `internal/*` imports

**Scenario 2: testify is confined to test files**
- **Given** testify is used for assertions in test files
- **When** `grep -r 'github.com/stretchr/testify' reconcile/ --include='*.go' | grep -v '_test.go'` is executed
- **Then** no results are returned — testify appears only in `*_test.go` files

**Scenario 3: golangci-lint is clean for the module**
- **Given** the `reconcile/` module is fully populated
- **When** `golangci-lint run ./reconcile/...` is executed
- **Then** the command exits 0 with no findings

## Edge Cases
**Edge Case 1: go mod tidy leaves empty require block**
- **Given** the `reconcile/go.mod` initially has no `require` block
- **When** `go mod tidy` is run inside `./reconcile/`
- **Then** the `go.mod` still has no `require` block (or an empty one), confirming zero external dependencies

**Edge Case 2: Library does not import any internal/* ATCR package**
- **Given** the library source files are moved from `internal/reconcile/`
- **When** `grep -r 'samestrin/atcr/internal' reconcile/` is executed
- **Then** no results are returned — the library has no dependency on ATCR internals

**Edge Case 3: time package is allowed for Options.ReconciledAt**
- **Given** the `Options` struct has a `ReconciledAt time.Time` field
- **When** the `time` import is checked
- **Then** `time` is a stdlib package and is permitted in non-test files

## Error Conditions
**Error Scenario 1: Third-party dependency leaked into production code**
- Error condition: a non-test `.go` file imports `github.com/stretchr/testify` or any other third-party package
- Symptom: `go mod tidy` in `./reconcile/` populates the `require` block
- Fix: move the import to a `*_test.go` file or replace the third-party call with stdlib equivalent

**Error Scenario 2: Library imports an ATCR internal package**
- Error condition: a file under `reconcile/` imports `github.com/samestrin/atcr/internal/stream` or any other `internal/*` path
- Symptom: `go build ./reconcile/...` fails with `use of internal package` error
- Fix: copy the needed pure function into the library (as done with `severity.go`) or refactor to pass data rather than import

**Error Scenario 3: levenshtein imported into library**
- Error condition: a file under `reconcile/` imports the `levenshtein` package from `internal/stream`
- Symptom: build failure or `require` block populated
- Fix: `levenshtein` stays in `internal/stream` (used only by ATCR path validation, not by dedupe); the library's dedupe uses token-set Jaccard, not edit distance

## Performance Requirements
- **Build Isolation:** `go build ./reconcile/...` succeeds with no network access (no module proxy needed)
- **Lint Speed:** `golangci-lint run ./reconcile/...` completes in under 10 seconds (small stdlib-only codebase)

## Security Considerations
- **Supply Chain:** Zero third-party dependencies means zero supply-chain attack surface in the library
- **Reproducibility:** No `go.sum` needed for the library module — builds are fully reproducible from stdlib alone

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:** No special test data — verification is structural (import analysis)
**Mock/Stub Requirements:** None
**Verification Commands:**
- `cd reconcile && go mod tidy && ! grep require go.mod` — require block absent
- `grep -r 'github.com/stretchr/testify' reconcile/ --include='*.go' | grep -v '_test.go'` — must return nothing
- `grep -r 'samestrin/atcr/internal' reconcile/` — must return nothing
- `golangci-lint run ./reconcile/...` — must exit 0

## Definition of Done
**Auto-Verified:**
- [x] `go build ./reconcile/...` exits 0
- [x] `golangci-lint run ./reconcile/...` is clean
- [x] `gofmt -l ./reconcile` produces no output

**Story-Specific:**
- [x] `go mod tidy` in `./reconcile/` leaves no `require` block
- [x] No non-test `.go` file under `reconcile/` imports any third-party package
- [x] No file under `reconcile/` imports any `github.com/samestrin/atcr/internal/*` path
- [x] testify imports appear only in `*_test.go` files

**Manual Review:**
- [x] Code reviewed and approved

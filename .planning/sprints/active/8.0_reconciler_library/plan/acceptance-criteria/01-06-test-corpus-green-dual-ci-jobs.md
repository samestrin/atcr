# Acceptance Criteria: Test Corpus Green with Dual CI Jobs

**Related User Story:** [01: Preserve ATCR as the Reference Implementation with Zero Behavioral Change](../user-stories/01-reference-implementation-preservation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | CI workflows (GitHub Actions) + go test corpus | Root job + new library-module job |
| Test Framework | go test + testify; `go test -race` in library job | Root `go test ./...` does NOT cross nested module boundary |
| Key Dependencies | `github.com/samestrin/atcr/reconcile`, golangci-lint, gofmt | Both jobs must be green |

### Related Files (from codebase-discovery.json)
- `.github/workflows/ci.yml` - modify: root CI; runs `go test ./...` (root module only); must remain green
- `.github/workflows/reconcile-module.yml` - create: new CI job for the library module; on tag push runs `cd ./reconcile && gofmt -l . && golangci-lint run && go test -race ./...`
- `internal/reconcile/emit_test.go` - reference: `TestReconcile_TwoReviewersAgreeHighConfidence` (`internal/reconcile/emit_test.go:20`) stays ATCR-internal; exercises the adapter end-to-end
- `internal/reconcile/cluster_merge_test.go` - reference: `MergeJSONFindings_VerificationPrecedence` validates merge ordering through the adapter
- `internal/reconcile/disagree_test.go` - reference: `BuildDisagreements` validates disagreement output through the adapter
- `internal/reconcile/gate.go` - reference: `IsFailing`/`CountAtOrAbove` (`internal/reconcile/gate.go:96`) stay ATCR-internal; import library `Verification` + `Verdict` constants
- `internal/reconcile/validate.go` - reference: `validateFindingPaths` (`internal/reconcile/validate.go:21`) stays ATCR-internal

## Design References
- [Adversarial Verification Interface](../../specifications/design-concepts/adversarial-verification-interface.md) — gate semantics and confidence v2 ordering exercised by the test corpus through `internal/reconcile/gate.go`.

## Happy Path Scenarios
**Scenario 1: Root test corpus green with zero behavioral change**
- **Given** the existing ATCR test corpus (`internal/reconcile/*_test.go`, `internal/debate`, `internal/verify`, `internal/report`, etc.) passes on `main` before extraction
- **When** the extraction lands and consumers import the library through the adapter
- **Then** `go test ./...` in the root module is green (exit 0) with the same test count and zero behavioral change

**Scenario 2: Library module tested via its own CI job**
- **Given** root `go test ./...` does NOT cross the nested module's `go.mod` boundary
- **When** the new `.github/workflows/reconcile-module.yml` runs `cd ./reconcile && go test -race ./...`
- **Then** the library module's own tests pass (exit 0), including `-race` detection, covering any library-internal test files

**Scenario 3: Both CI jobs green on the extraction PR**
- **Given** the extraction PR is opened
- **When** CI runs the root `.github/workflows/ci.yml` and the new `.github/workflows/reconcile-module.yml`
- **Then** both jobs report green; the extraction cannot merge until both pass (dual-job gate)

**Scenario 4: Library CI runs gofmt and golangci-lint**
- **Given** the library-module CI job
- **When** it runs `gofmt -l .` and `golangci-lint run` in `./reconcile/`
- **Then** both are clean (gofmt produces no output; golangci-lint exit 0), enforcing stdlib-only-pure code style in the library

## Edge Cases
**Edge Case 1: ATCR-internal corpus exercises the adapter end-to-end**
- **Given** `internal/reconcile/emit_test.go:20` (`TestReconcile_TwoReviewersAgreeHighConfidence`) and `cluster_merge_test.go`/`disagree_test.go` stay ATCR-internal
- **When** they run in the root test job
- **Then** they exercise the adapter boundary (not the library directly), proving end-to-end behavior through the ATCR consumer path

**Edge Case 2: gate.go and validate.go stay ATCR-internal**
- **Given** `gate.go` (`IsFailing`/`CountAtOrAbove`, `:96`) and `validate.go` (`validateFindingPaths`, `:21`) are ATCR-internal
- **When** the extraction lands
- **Then** these files import the library's `Verification` + `Verdict` constants (already exported, no visibility change) and their tests pass unchanged

**Edge Case 3: Library has no ATCR-internal test dependencies**
- **Given** the library module's `*_test.go` files
- **When** `go test ./...` runs in `./reconcile/`
- **Then** the library tests do not import `internal/reconcile`, `internal/stream`, or any ATCR-internal package (the library is self-contained)

## Error Conditions
**Error Scenario 1: Root test corpus regresses**
- Error message: `go test ./...` fails in the root module (a test in the existing corpus fails)
- HTTP status / error code: CI exit code 1; PR blocked from merge

**Error Scenario 2: Library module test job fails**
- Error message: `cd ./reconcile && go test -race ./...` fails (race detected or test failure)
- HTTP status / error code: `.github/workflows/reconcile-module.yml` exit code 1; PR blocked from merge

**Error Scenario 3: Library regression hidden because root job doesn't cross the boundary**
- Error message: a library-only test fails but the root job is green (the library job was skipped or not triggered)
- HTTP status / error code: CI configuration error; the dual-job gate must require BOTH jobs, not just one

**Error Scenario 4: gofmt or golangci-lint failure in the library**
- Error message: `gofmt -l .` outputs files needing formatting, or `golangci-lint run` reports issues
- HTTP status / error code: `.github/workflows/reconcile-module.yml` exit code 1

## Performance Requirements
- **Response Time:** Root `go test ./...` runtime must not regress by more than 5% against the pre-extraction baseline. Library `go test -race ./...` should complete in seconds (small stdlib-only module).
- **Throughput:** N/A (test execution, not runtime throughput)

## Security Considerations
- **Authentication/Authorization:** N/A (CI runs on PRs; no auth boundary)
- **Input Validation:** The dual-job gate is a safety control — it prevents a library regression from hiding behind a green root job. Both jobs must be required status checks on the PR protection rules.

## Test Implementation Guidance
**Test Type:** E2E (CI-driven; the corpus IS the test)
**Test Data Requirements:** The existing ATCR test corpus (no new tests except the adapter RED tests from AC 01-02). The pre-extraction test count is the baseline.
**Mock/Stub Requirements:** None. Verification: root `go test ./...` exit 0 with unchanged test count; `cd ./reconcile && go test -race ./...` exit 0; `gofmt -l .` empty output; `golangci-lint run` exit 0. Confirm both workflows are required status checks.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (root `go test ./...` exit 0; library `go test -race ./...` exit 0)
- [x] No linting errors (golangci-lint clean in both root and `./reconcile/`; gofmt clean in `./reconcile/`)
- [x] Build succeeds

**Story-Specific:**
- [x] Root `.github/workflows/ci.yml` green with unchanged test count (zero behavioral change)
- [x] New `.github/workflows/reconcile-module.yml` created and green (gofmt + golangci-lint + `go test -race`)
- [ ] Both jobs are required status checks on the PR protection rules (dual-job gate)
- [x] `gate.go` and `validate.go` stay ATCR-internal, importing library `Verification`/`Verdict` constants unchanged

**Manual Review:**
- [x] Code reviewed and approved
- [x] Confirm the library job triggers on tag push (or PR) and runs from `./reconcile/`

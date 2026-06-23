# Acceptance Criteria: PR-Time Module Test Job Closes Nested-Module Boundary Gap

**Related User Story:** [06: Independent Module CI and Leaderboard Reference Citation](../user-stories/06-independent-module-ci-leaderboard-citation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | GitHub Actions workflow step/job added to existing `ci.yml` | PR/push coverage for the nested module — fast feedback, not the full release gate |
| Test Framework | `go test ./...` (no race, no lint) inside `./reconcile` | Deliberately lightweight so PR feedback stays fast; the heavy gate runs on tag push (AC 06-01) |
| Key Dependencies | `actions/checkout@v4`, `actions/setup-go@v5` (Go 1.25), `[gauntlet]` self-hosted runner | Reuses ci.yml's existing setup |
| Trigger | `on: [push, pull_request]` (inherited from ci.yml) | Catches a `./reconcile` regression at PR time, not at the next tag |

## Related Files
- `.github/workflows/ci.yml` - modify: add a PR/push job (or step) that cds into `./reconcile` and runs `go test ./...`, reusing the `[gauntlet]` runner and Go 1.25 setup; root `go test ./...` does NOT cross the nested module's `go.mod` boundary.
- `.github/workflows/reconcile-module.yml` - reference: the tag-push companion (AC 06-01) runs the full gofmt + golangci-lint + race gate; the PR-time job runs `go test` only so PR feedback stays fast.
- `reconcile/go.mod` - read: the nested module boundary — the reason root `go test ./...` leaves the library untested; the PR job cds here to close the gap.
- `.githooks/pre-commit` or `.githooks/pre-push` - reference: the local hook mirror (if present) should also exercise `./reconcile` so the same gap does not reopen locally.

## Happy Path Scenarios
**Scenario 1: PR-time job runs `go test` inside `./reconcile`**
- **Given** a PR is opened or a push lands on `main`
- **When** ci.yml's new module-test job runs `cd ./reconcile && go test ./...`
- **Then** the library module's tests pass (exit 0) on the `[gauntlet]` runner with Go 1.25, so the nested module is tested on every PR — not just on tag push

**Scenario 2: Root `go test ./...` does NOT cross the nested module boundary**
- **Given** the root module's `go.mod` and the nested `./reconcile/go.mod` are separate module boundaries
- **When** ci.yml's existing root step runs `go test ./...`
- **Then** it tests the root module only and does NOT descend into `./reconcile`'s packages — which is exactly why the dedicated PR-time job is required

**Scenario 3: A deliberate break inside `./reconcile` is caught at PR time**
- **Given** a regression is introduced inside `./reconcile` (e.g. a test assertion is flipped to fail)
- **When** a PR carrying that break is opened
- **Then** the PR-time module-test job fails (exit 1) while the root `go test ./...` step stays green — proving the nested-module boundary gap is closed only by the dedicated job

**Scenario 4: PR-time job reuses ci.yml's runner and Go setup**
- **Given** ci.yml already runs on `[self-hosted, gauntlet]` with `actions/setup-go@v5` Go 1.25
- **When** the new job is added
- **Then** it reuses the same runner label and Go setup — no new runners or Go versions are provisioned by this change

## Edge Cases
**Edge Case 1: Boundary reason is documented inline**
- **Given** a future contributor might remove the dedicated job thinking root `go test ./...` covers `./reconcile`
- **When** the workflow is reviewed
- **Then** an inline comment documents the boundary reason (`# root go test ./... does NOT cross ./reconcile's go.mod boundary`), so the gap does not silently reopen

**Edge Case 2: PR-time job runs `go test` only (not the full gate)**
- **Given** the full release gate (gofmt + golangci-lint + race) runs on tag push (AC 06-01)
- **When** the PR-time job is authored
- **Then** it runs `go test ./...` only (no `-race`, no lint) so PR feedback stays fast and the heavy gate stays on tag push — two distinct coverage levels, not duplicated work

**Edge Case 3: Working directory is set correctly**
- **Given** the job must run inside `./reconcile` for `go test` to resolve the nested `go.mod`
- **When** the step is configured
- **Then** it uses `working-directory: ./reconcile` (or an explicit `cd ./reconcile &&`) so the module's `go.mod` is the resolution root — otherwise `go test` resolves against the root module and the gap reopens

**Edge Case 4: Module has no test files**
- **Given** the module scaffold lands before library-internal tests exist
- **When** `go test ./...` runs in `./reconcile`
- **Then** Go reports `no test files` and exits 0 (not a failure); the job still proves the module compiles and resolves under its own `go.mod`

## Error Conditions
**Error Scenario 1: A `./reconcile` regression is NOT caught by root `go test ./...`**
- Error message: root `go test ./...` exits 0 (green) while the module job exits 1
- HTTP status / error code: PR blocked by the module-test job (required status check); the deliberate-break proof confirms root coverage does not cross the boundary

**Error Scenario 2: PR-time job is missing and the gap reopens**
- Error message: a `./reconcile` test fails but no CI step catches it (root `go test ./...` is green and no module job exists)
- HTTP status / error code: CI configuration error; the dedicated job must exist and be a required status check so the gap cannot silently reopen

**Error Scenario 3: Job runs from the wrong directory**
- Error message: `go test ./...` resolves against the root module (no `working-directory` or `cd`), so `./reconcile` packages are not tested
- HTTP status / error code: silent CI configuration error; the step must cd into `./reconcile` or the boundary gap reopens

**Error Scenario 4: Go module resolution fails inside `./reconcile`**
- Error message: `go: cannot find module in ./reconcile` or `go.mod` parse error
- HTTP status / error code: module-test job exits 1; indicates the module scaffold (Stories 1-3) is not in place

## Performance Requirements
- **Response Time:** The PR-time module job should add minimal CI wall-clock — it runs `go test ./...` (no race, no lint) on a small stdlib-only module, completing in seconds. Go module/build cache (`cache: true` on setup-go) should be reused from ci.yml.
- **Throughput:** N/A (PR-time feedback, not runtime throughput).

## Security Considerations
- **Authentication/Authorization:** Runs with `permissions: contents: read` (inherited from ci.yml) — no write tokens needed.
- **Input Validation:** The job must be a required status check on PR protection rules; otherwise a `./reconcile` regression can merge untested. The inline boundary comment is a documentation control against accidental removal.
- **No Secrets:** The module-test job runs `go test` only and requires no secrets.

## Test Implementation Guidance
**Test Type:** E2E (CI-driven; the deliberate-break proof is the test)
**Test Data Requirements:** A PR carrying a deliberate failing test inside `./reconcile` (e.g. flip an assertion in a library `*_test.go`). The proof: the module-test job fails while the root `go test ./...` step stays green.
**Mock/Stub Requirements:** None. Verification: open a PR with a deliberate `./reconcile` break, confirm the module-test job catches it (exit 1) and root `go test ./...` stays green; revert the break and confirm both pass. Confirm the job is a required status check.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (module job `go test ./...` exits 0 inside `./reconcile` on a clean PR)
- [ ] No linting errors (the PR-time job itself does not run lint; lint is gated on tag push by AC 06-01)
- [ ] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [ ] ci.yml gains a PR/push job (or step) that cds into `./reconcile` and runs `go test ./...`
- [ ] The job reuses the `[gauntlet]` runner and Go 1.25 setup from ci.yml (no new runners)
- [ ] A deliberate `./reconcile` break is caught by the PR-time job while root `go test ./...` stays green (boundary-gap proof)
- [ ] The boundary reason is documented inline (`# root go test ./... does NOT cross ./reconcile's go.mod boundary`)
- [ ] The module-test job is a required status check on PR protection rules

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Confirm the job runs `go test` only (not the full gofmt + lint + race gate) so PR feedback stays fast

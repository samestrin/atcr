# Acceptance Criteria: Tag-Push Release Gate for Independent Module CI

**Related User Story:** [06: Independent Module CI and Leaderboard Reference Citation](../user-stories/06-independent-module-ci-leaderboard-citation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | GitHub Actions workflow (YAML) | New `.github/workflows/reconcile-module.yml` — the release gate for the standalone module |
| Test Framework | `gofmt -l .` + `golangci-lint run` + `go test -race ./...` | The three release-gate checks run inside `./reconcile` |
| Key Dependencies | `actions/checkout@v4`, `actions/setup-go@v5` (Go 1.25), `golangci/golangci-lint-action`, `[gauntlet]` self-hosted runner | `based_on: .github/workflows/ci.yml`; no new runners provisioned |
| Trigger | `on: push: tags:` scoped to the module's release-tag convention | Fires as the release gate (AC#7), not on every push |

### Related Files (from codebase-discovery.json)
- `.github/workflows/reconcile-module.yml` - create: tag-push release gate workflow; triggers on tag push, cds into `./reconcile`, and runs `gofmt` check + `golangci-lint` + `go test -race ./...` on the self-hosted `[gauntlet]` runner with Go 1.25.
- `.github/workflows/ci.yml` - reference: the workflow is `based_on` ci.yml — reuses its `runs-on: [self-hosted, gauntlet]` label, `actions/setup-go@v5` Go 1.25 setup, and `actions/checkout@v4` checkout step.
- `.golangci.yml` - read/create: repository-root lint config (vet, ineffassign, staticcheck, errcheck) governing the tag-push lint step so it is reproducible across CI and local runs.
- `reconcile/go.mod` - read: the nested module boundary; the workflow cds here (`working-directory: ./reconcile` or `cd ./reconcile`) so the release gate runs against the standalone module, not the root.

## Happy Path Scenarios
**Scenario 1: Workflow triggers on a module release tag push**
- **Given** a tag matching the project's release-tag convention is pushed (e.g. `reconcile/v0.1.0` or the existing tag prefix)
- **When** GitHub evaluates `.github/workflows/reconcile-module.yml`'s `on: push: tags:` filter
- **Then** the release-gate job is triggered and runs on the `[gauntlet]` self-hosted runner with Go 1.25

**Scenario 2: Release gate runs the three checks inside `./reconcile`**
- **Given** the release-gate job has checked out the repo and set up Go 1.25
- **When** it cds into `./reconcile` and runs `gofmt -l . && golangci-lint run --timeout 5m && go test -race ./...`
- **Then** all three checks pass (gofmt produces no output; golangci-lint exits 0; `go test -race` exits 0), proving the module is buildable and tested standalone — not transitively through ATCR

**Scenario 3: Workflow reuses the ci.yml runner and Go setup**
- **Given** `.github/workflows/ci.yml` already runs on `[self-hosted, gauntlet]` with `actions/setup-go@v5` Go 1.25
- **When** `reconcile-module.yml` is authored
- **Then** it copies the `runs-on`, checkout, and Go setup steps from ci.yml verbatim — no new runners, no new Go version, no third-party CI service is introduced

**Scenario 4: Race detector runs on the standalone module**
- **Given** the module's `*_test.go` files are self-contained (stdlib-only production, testify in tests)
- **When** `go test -race ./...` runs inside `./reconcile`
- **Then** the race detector passes (exit 0), catching concurrency issues in the reconciler's pure logic at release time

## Edge Cases
**Edge Case 1: Tag filter scopes to module releases, not ATCR app tags**
- **Given** the repository may carry tags for both the ATCR app and the module
- **When** an ATCR app tag (not a module release) is pushed
- **Then** `reconcile-module.yml` does NOT trigger (the `on: push: tags:` filter excludes it), avoiding noise or false failures on the wrong artifact

**Edge Case 2: golangci-lint version is pinned and consistent with ci.yml**
- **Given** the story specifies golangci-lint `v2.12.2` while the existing ci.yml pins `v2.6.2` via `golangci/golangci-lint-action@v8`
- **When** `reconcile-module.yml` is authored
- **Then** golangci-lint is pinned to an explicit version in the workflow, and that version matches the version used in ci.yml (or both are updated together in one change) so a tag cannot pass locally but fail in CI (or vice versa) due to drift

**Edge Case 3: `.golangci.yml` is shared so rules cannot drift**
- **Given** a single `.golangci.yml` lives at the repository root
- **When** the tag-push lint step and any local/pre-commit lint both run
- **Then** they read the same config file, so the linter selection (vet, ineffassign, staticcheck, errcheck) is identical everywhere

**Edge Case 4: Module has no test files yet**
- **Given** the module scaffold lands before library-internal tests exist
- **When** `go test -race ./...` runs in `./reconcile`
- **Then** Go reports `no test files` per package and exits 0 (not a failure), and the gofmt + golangci-lint checks still gate the release

## Error Conditions
**Error Scenario 1: gofmt finds unformatted files in the module**
- Error message: `gofmt -l .` lists one or more files (e.g. `cluster.go`)
- HTTP status / error code: workflow step exits 1; release gate fails; the tag is not a passing release

**Error Scenario 2: golangci-lint reports issues**
- Error message: `golangci-lint run --timeout 5m` reports lint issues (e.g. `errcheck: unchecked error`)
- HTTP status / error code: workflow exits 1; release gate fails

**Error Scenario 3: `go test -race` detects a data race or test failure**
- Error message: `WARNING: DATA RACE` or `--- FAIL: TestXxx`
- HTTP status / error code: `go test -race` exits 1; release gate fails; the race is caught before the module is released

**Error Scenario 4: Workflow triggers on the wrong tags**
- Error message: `reconcile-module.yml` fires on an ATCR app tag and fails because `./reconcile` is unrelated to that artifact
- HTTP status / error code: CI configuration error; the `on: push: tags:` filter must be scoped to the module's tag prefix (verify against recent tag history before finalizing)

**Error Scenario 5: Runner or Go setup unavailable**
- Error message: `[gauntlet]` self-hosted runner is offline or Go 1.25 is not set up
- HTTP status / error code: job fails for environment reasons, not code reasons; both jobs reuse ci.yml's setup verbatim so the issue surfaces on root CI first

## Performance Requirements
- **Response Time:** The release-gate job should complete in well under the ci.yml test runtime — the module is a small stdlib-only package. `go test -race ./...` should finish in seconds; `golangci-lint run --timeout 5m` is bounded by its timeout.
- **Throughput:** N/A (release-gate execution, not runtime throughput).

## Security Considerations
- **Authentication/Authorization:** The workflow runs with `permissions: contents: read` (matching ci.yml) — no write tokens are needed for a test/lint gate. Tag push is gated by repository release permissions, not by this workflow.
- **Input Validation:** The `on: push: tags:` filter must be scoped to the module's release-tag convention so the gate cannot be triggered spuriously by unrelated tags. Verify the filter against recent tag history before finalizing.
- **No Secrets:** The release-gate job must not require or expose secrets; it runs `gofmt`/`golangci-lint`/`go test` only.

## Test Implementation Guidance
**Test Type:** E2E (CI-driven; the workflow IS the test)
**Test Data Requirements:** A module release tag matching the project's tag convention, and a `./reconcile` module with its own `go.mod` (delivered by Stories 1-3). No new test data is generated by this AC.
**Mock/Stub Requirements:** None. Verification: push a tag, confirm `reconcile-module.yml` triggers on `[gauntlet]` with Go 1.25, cds into `./reconcile`, and all three checks pass. Locally, replicate with `cd ./reconcile && gofmt -l . && golangci-lint run --timeout 5m && go test -race ./...`. Confirm the `on: push: tags:` filter does NOT fire on ATCR app tags.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test -race ./...` exits 0 inside `./reconcile`)
- [x] No linting errors (gofmt clean; golangci-lint exits 0)
- [x] Build succeeds (`go build ./reconcile/...`)

**Story-Specific:**
- [x] `.github/workflows/reconcile-module.yml` exists and triggers on tag push (release gate, AC#7)
- [x] Workflow cds into `./reconcile` and runs all three checks: gofmt + golangci-lint + `go test -race`
- [x] Workflow reuses `[gauntlet]` runner and Go 1.25 from ci.yml (no new runners or Go versions)
- [x] golangci-lint is pinned to an explicit version consistent with ci.yml (no drift)
- [x] `on: push: tags:` filter is scoped to the module's release-tag convention (verified against recent tag history)

**Manual Review:**
- [x] Code reviewed and approved
- [x] Confirm the workflow is `based_on` ci.yml and the runner/Go setup matches verbatim

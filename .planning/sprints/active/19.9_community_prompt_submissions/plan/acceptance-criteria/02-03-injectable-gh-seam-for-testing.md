# Acceptance Criteria: Injectable `gh` Seam Matching `personasClient`/`personasFixtureRunner` Conventions

**Related User Story:** [2: Fork + PR Automation via `gh`](../user-stories/02-fork-and-pr-automation-via-gh.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go interface or package-level var (seam) | e.g. `personasGitHub` package var of an interface type with `Fork`, `PushBranch`, `CreatePR` (and precondition check) methods, defaulting to a `gh.ExecContext`-backed implementation |
| Test Framework | Go `testing` package | Tests substitute a stub implementation via the package var, matching `personasClient`/`personasFixtureRunner` in `cmd/atcr/personas.go`; testify is not used in this codebase |
| Key Dependencies | `github.com/cli/go-gh/v2` (default implementation only), no new dependency for the seam itself | Interface lives independent of `go-gh` so stubs need not import it |

### Related Files (from codebase-discovery.json)
- `cmd/atcr/personas.go` (modify) — declare a `personasGitHub` package var (interface-typed) alongside the existing `personasDir`/`personasClient`/`personasFixtureRunner` seams, used by `newPersonasSubmitCmd`
- `internal/personas/submit.go` (create) — define the seam interface (e.g. `GitHubSubmitter`) and its default `gh.ExecContext`-backed implementation
- `cmd/atcr/personas_test.go` (modify) — add a stub `GitHubSubmitter` implementation used to unit-test `newPersonasSubmitCmd` without a real `gh` binary or network call
- `internal/personas/submit_test.go` (create) — unit tests against the seam interface directly (not through the cobra command), verifying stub substitution fully replaces the default implementation
- `internal/ghaction/client.go` (reference only) — existing fixed-bot GitHub REST client (lines 192, 345) that must remain independent of the new `GitHubSubmitter` seam

## Design References
- [GitHub Fork + PR Integration via go-gh](../documentation/gh-fork-pr-integration.md) — why the `gh` CLI shell-out approach is used instead of `internal/ghaction.Client`
- [Cobra Subcommand & Injectable-Seam Conventions](../documentation/cobra-subcommand-patterns.md) — the `personasDir`/`personasClient`/`personasFixtureRunner` injectable-seam pattern the new `personasGitHub` seam must follow

## Happy Path Scenarios
**Scenario 1: Tests substitute a stub seam with no real `gh` binary present**
- **Given** a test environment where `gh` may not even be installed
- **When** `cmd/atcr/personas_test.go` sets the `personasGitHub` package var to a stub recording calls and returning canned results
- **Then** `newPersonasSubmitCmd`'s `RunE` exercises the full precondition-check-through-PR-create flow using only the stub, with no `exec.Command`/`gh.ExecContext` call reaching a real process

**Scenario 2: Default production implementation wires to `gh.ExecContext`**
- **Given** the package is used unmodified (no test override)
- **When** `atcr personas submit <name>` runs in a real environment
- **Then** the default `GitHubSubmitter` implementation shells out via `gh.ExecContext(ctx, ...)` for `gh.Path()`/`auth status`/`repo fork`/`pr create`, and plain `git`/branch-push operations for the push step, consistent with `documentation/gh-fork-pr-integration.md`

## Edge Cases
**Edge Case 1: Seam is restored after each test**
- **Given** a test overrides `personasGitHub` (or `checkGHPrecondition`'s underlying seam) to a stub
- **When** the test completes
- **Then** the test restores the original package var (via `t.Cleanup` or equivalent), matching the existing restoration pattern already used for `personasClient`/`personasFixtureRunner` overrides in this file, so seam state does not leak between test cases

**Edge Case 2: Seam decouples `internal/ghaction.Client` from this flow entirely**
- **Given** the fixed-bot `internal/ghaction.Client` (internal/ghaction/client.go:192, :345) exists in the same codebase and is used by the Epic 17.0 `--auto-fix` flow
- **When** the new seam is introduced for `personas submit`
- **Then** the seam's interface, default implementation, and package var are defined independently of `internal/ghaction.Client` — no shared type, no shared package var — so a change to one flow cannot accidentally alter the other's behavior

## Error Conditions
**Error Scenario 1: Stub seam simulates a mid-sequence failure**
- **Given** a stub `GitHubSubmitter` configured to fail on `CreatePR` after succeeding on `Fork`/`PushBranch`
- **Error message:** whatever the stub is configured to return, propagated unchanged by `newPersonasSubmitCmd` to the user
- **Then** the test can assert the command surfaces that exact error and exits non-zero, without needing a real GitHub API failure to occur

**Error Scenario 2: Missing seam override in a test accidentally invokes the real `gh` binary**
- **Given** a test forgets to override `personasGitHub` before invoking `newPersonasSubmitCmd`
- **Then** CI environments without `gh` installed or without network access exit immediately with a non-zero status and a clear `gh.Path()` "not found" or network error (from AC 02-01's precondition check) rather than hanging or silently skipping — surfacing the missing-stub mistake immediately rather than masking it

## Performance Requirements
- **Response Time:** Stub-backed unit tests must run in milliseconds (no real subprocess or network I/O), consistent with the rest of the `internal/personas` unit-test suite
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** The seam interface never exposes or requires direct token/credential handling — the default implementation relies entirely on `gh`'s own credential resolution (per AC 02-01); stub implementations in tests never touch real credentials
- **Input Validation:** The seam's method signatures (e.g. `Fork(ctx, repo)`, `PushBranch(ctx, branchName, files)`, `CreatePR(ctx, req)`) take structured, typed arguments rather than raw shell strings, preventing a caller from constructing an injectable `gh.ExecContext` argument list ad hoc

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A minimal stub `GitHubSubmitter` implementation (in-memory call recorder) reused across AC 02-01 and AC 02-02 tests
**Mock/Stub Requirements:** The seam interface itself is the mock boundary; no `httptest` server or subprocess mocking library is needed since the interface fully replaces the `gh` interaction surface

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A package-level seam (interface or var) wraps all `gh` interaction used by `personas submit`, following the `personasClient`/`personasFixtureRunner` pattern
- [ ] Unit tests fully exercise `newPersonasSubmitCmd` via a stubbed seam with zero real `gh` process invocations or network calls
- [ ] The seam is defined independently of `internal/ghaction.Client`, with no shared types or package vars between the two integration points
- [ ] Test overrides of the seam are restored after each test (no cross-test leakage)

**Manual Review:**
- [ ] Code reviewed and approved

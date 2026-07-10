# Acceptance Criteria: `gh` Precondition Check (PATH + Auth) Before Any Fork/Branch Work

**Related User Story:** [2: Fork + PR Automation via `gh`](../user-stories/02-fork-and-pr-automation-via-gh.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (CLI precondition check) | `checkGHPrecondition(ctx context.Context) error` invoked at the top of `newPersonasSubmitCmd`'s `RunE`, before any fork/branch/commit call |
| Test Framework | `go test` + `testify` (`require`/`assert`) | Matches existing `cmd/atcr/personas_test.go` and `internal/personas/*_test.go` conventions |
| Key Dependencies | `github.com/cli/go-gh/v2` (`gh.Path()`, `gh.ExecContext`), `github.com/spf13/cobra` | `go-gh/v2` is a new module dependency (not yet in `go.mod`); no custom OAuth/token logic is introduced |

## Related Files
- `cmd/atcr/personas.go` - modify: add `newPersonasSubmitCmd()` whose `RunE` calls the precondition check first, before fork/branch/PR work, halting on failure
- `internal/personas/submit.go` - create: `checkGHPrecondition(ctx context.Context) error`, implementing the `gh.Path()` + `gh auth status` check documented in `documentation/gh-fork-pr-integration.md`
- `cmd/atcr/personas_test.go` - modify: add test coverage asserting the precondition check runs before the fork seam is invoked
- `skill/SKILL.md` - reference only: source of the "verify binary/tool available, halt with actionable message" precondition pattern (line 18) this check mirrors

## Happy Path Scenarios
**Scenario 1: `gh` on PATH and authenticated**
- **Given** the invoking user has `gh` installed on `PATH` and has an active `gh auth login` session
- **When** `atcr personas submit <name>` runs after `commpersonas.TestPersona` has passed for `<name>`
- **Then** `checkGHPrecondition` returns `nil` and the command proceeds to the fork/branch/push/PR-create sequence (AC 02-02) without any additional user interaction

## Edge Cases
**Edge Case 1: `gh` on PATH but not authenticated**
- **Given** `gh.Path()` resolves successfully but no active session exists
- **When** `checkGHPrecondition` runs `gh.ExecContext(ctx, "auth", "status")`
- **Then** the non-zero exit is surfaced as an error whose message includes the captured stderr (e.g. reason "You are not logged into any GitHub hosts") and directs the user to run `gh auth login`; no fork/branch/PR call is made

**Edge Case 2: `gh auth status` succeeds but for a different host than the target repo**
- **Given** the user is authenticated to a `gh` host other than `github.com`
- **When** the precondition check runs
- **Then** the check only verifies *an* active session exists (matching the documented `gh auth status` contract) — host/repo-target validation is deferred to the fork step (AC 02-02) via `pkg/repository.Parse("samestrin/atcr")`, so this AC's scope stays limited to "is `gh` present and authenticated," not "is it authenticated for this specific repo"

## Error Conditions
**Error Scenario 1: `gh` binary not found on PATH**
- **Given** `gh.Path()` returns an error (binary absent)
- **When** `atcr personas submit <name>` runs
- **Then** the command halts before any fork/branch/PR call and returns a non-zero exit with error message: `"gh CLI not found on PATH; install it from https://cli.github.com"`

**Error Scenario 2: `gh auth status` fails (no active session or expired token)**
- **Given** `gh` is on `PATH` but `gh auth status` exits non-zero
- **When** the precondition check runs
- **Then** the command halts with error message: `"gh auth check failed: <captured stderr>"` and no fork/branch/commit work is attempted

## Performance Requirements
- **Response Time:** The precondition check must complete within the same process invocation (two subprocess calls: `gh.Path()` resolution is in-process; `gh auth status` is a single external process call), typically under 2 seconds on a warm `gh` install with no network dependency for local auth-cache checks
- **Throughput:** N/A (single-invocation CLI command, not a service)

## Security Considerations
- **Authentication/Authorization:** The check never reads, stores, or logs the user's `gh` token; it only inspects the exit code and stderr of `gh auth status`, which itself redacts credentials
- **Input Validation:** No user-supplied input is passed to `gh.Path()` or `gh auth status` — both are fixed, argument-free invocations, eliminating command-injection risk at this check

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** No real `gh` binary or network access required; tests stub the seam that wraps `gh.Path()`/`gh.ExecContext` to return canned success/failure results
**Mock/Stub Requirements:** A package-level seam (see AC 02-03) exposing `Path()` and `AuthStatus(ctx)` as swappable functions/interface methods, so unit tests can simulate "not on PATH" and "not authenticated" without invoking a real `gh` process

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `checkGHPrecondition` runs before any fork/branch/push/PR-create call in `newPersonasSubmitCmd`
- [ ] Missing-`gh`-on-PATH halts with the exact actionable message including the install URL
- [ ] Failed `gh auth status` halts with an error message that surfaces the captured stderr
- [ ] Unit tests cover both precondition failure modes without invoking a real `gh` binary or network call

**Manual Review:**
- [ ] Code reviewed and approved

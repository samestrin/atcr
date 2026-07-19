# Acceptance Criteria: Zero Behavior Change to the runAutoFix Pipeline

**Related User Story:** [01: Route Auto-Fix Validation Through the Sandbox by Default](../user-stories/01-route-autofix-validation-through-sandbox.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go integration test at the `runAutoFix` orchestration level (`cmd/atcr/autofix.go`) | Exercises the full apply → validate → revert-or-continue → branch/commit/PR pipeline with a fake `sandbox.Backend` substituted for the host-exec validation path |
| Test Framework | `go test` + `testify`, reusing the existing `runAutoFix` test fixtures (call-recording `autoFixGitHub` fake, `autofix.ApplyPatch`/`RevertPatch`) | No new test framework introduced |
| Key Dependencies | `internal/autofix` (`ApplyPatch`, `RevertPatch`, `CleanupBackups`), `internal/ghaction` (`CommitRequest`, `PullRequestRequest`), `internal/sandbox.Backend` (fake) | None of these packages are modified by this story |

## Related Files
- `cmd/atcr/autofix.go` - modify: the sole call-site swap at line 252 (`verify.RunConfiguredValidation(...)` → the sandbox-routing equivalent from AC 01-01/01-02), with the three post-call branches (`verr != nil` at line 253, `!res.Passed()` at line 260, success/`CleanupBackups` at line 269) requiring zero source changes.
- `cmd/atcr/autofix_test.go` - modify: add (or extend) `runAutoFix` test cases that substitute a fake `sandbox.Backend` for the validation step and assert `ApplyPatch`/`RevertPatch`/`CleanupBackups` and the branch/commit/PR call sequence (`CreateBranch` → `CreateCommit` → `FindOpenPullRequest` → `CreatePullRequest`/`UpdatePullRequest`) are unchanged in both the success and failure cases.
- `internal/autofix/autofix.go` (or wherever `ApplyPatch`/`RevertPatch`/`CleanupBackups` are defined) - reference only: this AC proves these are never touched by the routing change; no modification expected here.
- `internal/verify/localvalidate_test.go` - reference only: this AC's "provably unaffected" requirement is partly satisfied by confirming this file's existing host-path tests (`TestRunConfiguredValidation_*`) continue to pass unmodified, proving the host `os/exec` code path itself is untouched.

## Happy Path Scenarios
**Scenario 1: Sandboxed validation passes — full pipeline proceeds to PR creation unchanged**
- **Given** `runAutoFix` is invoked with a backend wired to route validation through a fake `sandbox.Backend` that returns a passing result (`Passed() == true`)
- **When** `runAutoFix` executes
- **Then** `ApplyPatch` is called once, `CleanupBackups` is called once, and the GitHub call sequence (`CreateBranch` → `CreateCommit` → `FindOpenPullRequest` → `CreatePullRequest` or `UpdatePullRequest`) occurs exactly as it does today with a passing host-exec result — byte-identical call arguments and ordering, verified against a call-recording fake

**Scenario 2: Sandboxed validation fails — revert occurs, no GitHub call is ever made**
- **Given** the fake `sandbox.Backend` returns a failing result (`Passed() == false`, non-zero exit)
- **When** `runAutoFix` executes
- **Then** `RevertPatch` is called and the function returns the same `"auto-fix: local validation failed (exit %d); working tree reverted, no GitHub changes made"` error format (`cmd/atcr/autofix.go:264`) with the correct exit code interpolated, and none of `CreateBranch`/`CreateCommit`/`FindOpenPullRequest`/`CreatePullRequest`/`UpdatePullRequest` are invoked

**Scenario 3: Sandboxed validation cannot even start — same "cannot validate" wording and revert path as the host-exec case**
- **Given** the fake `sandbox.Backend.Run` returns a non-nil error (translated to `ValidationResult.StartError` per AC 01-02)
- **When** `runAutoFix` executes
- **Then** `RevertPatch` is called and the function returns the same `"auto-fix: cannot run validation (working tree reverted, no GitHub changes made): %w"` error format (`cmd/atcr/autofix.go:258`), wrapping the translated error, and no GitHub call is made — identical to today's behavior when `verify.RunConfiguredValidation` itself returns a non-nil error

## Edge Cases
**Edge Case 1: Apply failure short-circuits before validation is ever reached (sandboxed or not)**
- **Given** `autofix.ApplyPatch` fails
- **When** `runAutoFix` executes
- **Then** `RevertPatch` is called and the function returns before any validation call (sandbox or host) is made — this path is untouched by the routing change and must remain covered by existing tests without modification

**Edge Case 2: Revert itself fails after a sandboxed validation failure**
- **Given** the fake `sandbox.Backend` returns a failing result AND `autofix.RevertPatch` also fails
- **When** `runAutoFix` executes
- **Then** the combined error format `"auto-fix: validation failed AND revert failed: %w"` (`cmd/atcr/autofix.go:262-263`) is returned unchanged, regardless of which execution path (host or sandbox) produced the validation failure

**Edge Case 3: Empty base branch after a sandboxed validation pass**
- **Given** the fake `sandbox.Backend` returns a passing result AND `run.Base` is empty
- **When** `runAutoFix` executes
- **Then** the existing guard at `cmd/atcr/autofix.go:274-276` still fires with `"auto-fix: no base branch resolved for the pull request; no GitHub changes made"`, proving the post-validation branch/commit/PR guards are unaffected by the routing change

## Error Conditions
**Error Scenario 1: Backup-cleanup-and-revert combined failure wording is preserved**
- Error message: `"auto-fix: cannot validate AND revert failed: %w; validation error: %v"` (`cmd/atcr/autofix.go:256`) — verified reachable identically whether the underlying "cannot validate" cause originated from the host `os/exec` path or the sandbox `StartError` translation
- HTTP status / error code: N/A (CLI exit code / wrapped Go error only)

**Error Scenario 2: No behavior regression in `commitFilesFrom` / commit content**
- Error message: N/A — this AC asserts (not error-triggers) that the files committed via `gh.CreateCommit` after a passing sandboxed validation contain the same post-apply on-disk content as after a passing host validation, since `commitFilesFrom` (`cmd/atcr/autofix.go:318-333`) reads from `be.applyTarget` on disk regardless of which execution path validated it
- HTTP status / error code: N/A

## Performance Requirements
- **Response Time:** No regression budget change — `runAutoFix`'s own orchestration overhead (excluding the validation call's intrinsic duration, which is bounded by `validateTimeout`/`RunSpec.Timeout` per AC 01-01) must remain effectively zero-overhead compared to today.
- **Throughput:** N/A — `runAutoFix` remains a single sequential pipeline per invocation; no new concurrency is introduced anywhere in this call chain.

## Security Considerations
- **Authentication/Authorization:** No change to how GitHub token/owner/repo are resolved or used (`validateAutoFixBackend`, `cmd/atcr/autofix.go:107-192`) — entirely out of scope for this story and must remain untouched.
- **Input Validation:** No change to `payload.BuildEntriesFromDiff`, `selectAutoFixEntries`, or any diff-parsing/threshold logic (`cmd/atcr/autofix.go:412-467`) — this AC's regression tests confirm those functions are not exercised differently by the routing change.

## Test Implementation Guidance
**Test Type:** INTEGRATION
**Test Data Requirements:** Reuse (or closely mirror) the existing `runAutoFix` test harness's temp-directory fixtures, `payload.FileEntry` sets, and call-recording `autoFixGitHub` fake; substitute only the validation step's execution path (fake `sandbox.Backend` in place of a real/host command) while keeping every other input identical to the pre-existing host-path test cases, so the two sets of tests are provably parallel.
**Mock/Stub Requirements:** Fake `sandbox.Backend` (configurable `Run` return value/error) for the validation step; existing call-recording `autoFixGitHub` fake for `CreateBranch`/`CreateCommit`/`FindOpenPullRequest`/`CreatePullRequest`/`UpdatePullRequest`; real `autofix.ApplyPatch`/`RevertPatch`/`CleanupBackups` against a temp directory (no mocking of the file-mutation layer, consistent with existing tests).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Integration test proves a passing sandboxed validation drives the identical `ApplyPatch` → `CleanupBackups` → `CreateBranch` → `CreateCommit` → `FindOpenPullRequest`/`CreatePullRequest`/`UpdatePullRequest` sequence as the existing passing host-path test
- [ ] Integration test proves a failing sandboxed validation drives `RevertPatch` and the exact existing "validation failed" error wording, with zero GitHub calls made
- [ ] Integration test proves a sandbox `StartError` (cannot-validate) drives `RevertPatch` and the exact existing "cannot run validation" error wording, with zero GitHub calls made
- [ ] Full existing `cmd/atcr` auto-fix test suite (apply-failure, revert-failure, empty-base-branch, PR-create-vs-update) passes unmodified in outcome when re-run against the new sandboxed path

**Manual Review:**
- [ ] Code reviewed and approved

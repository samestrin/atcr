# Acceptance Criteria: `--no-sandbox` Bypasses Story 2's Resolver/Preflight Gate

**Related User Story:** [03: `--no-sandbox` Opt-Out Flag with CLI Security Warnings](../user-stories/03-no-sandbox-opt-out-flag.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Conditional branch inside `validateAutoFixBackend` (`cmd/atcr/autofix.go:107`); direct `os/exec` validation path in `runAutoFix` (`cmd/atcr/autofix.go:239`) | No new package; reads `cmd.Flags().GetBool("no-sandbox")` matching the existing pattern used for `--repo`/`--token`/`--api-url` at lines 167-179 |
| Test Framework | `go test`, table-driven, hermetic (no live Docker daemon) | Mirrors `internal/verify/exec_test.go`'s `fakeDocker` shim pattern referenced in Story 2 for proving no Docker call occurs |
| Key Dependencies | `internal/verify.ResolveAutoFixSandbox` (Story 2's resolver, mirroring `ResolveExecBackend` at `internal/verify/exec.go:24`), `internal/sandbox.Backend`, `internal/verify.RunConfiguredValidation` (pre-existing direct-exec path) | This story calls *around* Story 2's resolver, never modifying its signature |

## Related Files
- `cmd/atcr/autofix.go` - modify: `validateAutoFixBackend` (line 107) gains an early branch — when `noSandbox` is true, skip the call to `verify.ResolveAutoFixSandbox`/Preflight entirely (no Docker requirement, no `sandbox:` config requirement) and leave the resolved backend's sandbox field unset/nil
- `cmd/atcr/autofix.go` - modify: `runAutoFix` (line 239) — the validation call at line 252 (`verify.RunConfiguredValidation(ctx, be.validateArgv, be.applyTarget, be.validateTimeout)`) is the pre-existing direct `os/exec` path; when the resolved backend carries no sandbox (because `--no-sandbox` bypassed resolution), this call must remain the one actually executed, not routed through Story 1's sandboxed `Backend.Run`
- `cmd/atcr/autofix_test.go` - modify/create: table-driven test asserting `validateAutoFixBackend` with `--no-sandbox=true` never invokes the sandbox resolver (via a call-counting fake or by asserting no Docker/Preflight-shaped error appears) even when no `sandbox:` config block exists and no Docker binary is reachable
- `internal/verify/exec.go` - reference only: confirms `ResolveExecBackend`'s existing `execEnabled bool` early-return shape (`if !execEnabled { return nil, nil, 0, nil }`, lines 25-27) is the precedent this story's bypass branch should structurally match for Story 2's analogous resolver

## Happy Path Scenarios
**Scenario 1: `--no-sandbox` skips resolution with no Docker and no sandbox config**
- **Given** a project with no `sandbox:` block in `.atcr/config.yaml` and no `docker` binary on `PATH`
- **When** `validateAutoFixBackend` runs with `--auto-fix --no-sandbox` set
- **Then** it does not call Story 2's resolver/Preflight at all, appends no sandbox-related entry to `missing`, and returns a resolved `autoFixBackend` with `be.validateArgv`/`be.applyTarget`/`be.validateTimeout` populated as normal (apply-target, validation-command, and GitHub-credential checks are unaffected — this bypass is scoped to sandbox resolution only)

**Scenario 2: `runAutoFix` executes validation directly, not through a sandbox backend**
- **Given** a resolved `autoFixBackend` produced by the `--no-sandbox` path (no sandbox backend attached)
- **When** `runAutoFix` reaches its validation step
- **Then** `verify.RunConfiguredValidation` runs the configured validate command directly via `os/exec` against `be.applyTarget`, exactly as it did before Story 1 existed — proven by a test double/spy on the validation call path (e.g. a fake `validateArgv` like `["true"]` or `["false"]` whose exit code alone determines the result, with no sandbox-related error surfacing)

**Scenario 3: `--no-sandbox` combined with a valid `sandbox:` config still bypasses it**
- **Given** a project WITH a fully valid `sandbox:` config block (Image, TestCommand, Docker reachable)
- **When** `--auto-fix --no-sandbox` is passed
- **Then** the sandbox resolver is still never called — presence of a working sandbox config does not override an explicit `--no-sandbox`; the operator's explicit choice wins unconditionally

## Edge Cases
**Edge Case 1: `--no-sandbox` set but `--auto-fix` absent**
- **Given** `atcr review --no-sandbox` without `--auto-fix`
- **When** the review command runs
- **Then** `validateAutoFixBackend` is never called at all (it is only invoked on the `--auto-fix` path per existing `runReview` wiring), so the flag has zero effect — confirming the story's constraint that `--no-sandbox` is meaningless without `--auto-fix`

**Edge Case 2: Sandbox config present but malformed, with `--no-sandbox` set**
- **Given** a `sandbox:` block that would fail `registry.SandboxConfig.Validate()` (e.g. missing `test_command`)
- **When** `--auto-fix --no-sandbox` is passed
- **Then** the malformed sandbox config produces no error and no `missing` entry — validation of that config is never reached because resolution is skipped entirely, proving the bypass is unconditional and not merely "tolerant of failure"

**Edge Case 3: `--no-sandbox=false` explicitly passed**
- **Given** `atcr review --auto-fix --no-sandbox=false`
- **When** `validateAutoFixBackend` runs
- **Then** behavior is identical to the flag being entirely absent — Story 2's resolver/Preflight gate runs normally and a missing/failing sandbox is a hard refusal (regression check that the bypass branch is strictly gated on `true`, not on "flag was set")

## Error Conditions
**Error Scenario 1: Direct validation command fails under `--no-sandbox`**
- **Given** `--no-sandbox` bypassed sandbox resolution and the configured validate command exits non-zero
- **When** `runAutoFix` evaluates the `verify.RunConfiguredValidation` result
- **Then** the existing failure path fires unchanged: the patch is reverted via `autofix.RevertPatch`, no GitHub call is made, and the returned error reads `"auto-fix: local validation failed (exit %d); working tree reverted, no GitHub changes made"` (line 264) — `--no-sandbox` changes *where* validation runs, never the pass/fail handling contract
- HTTP status / error code: N/A (CLI exit code 1, per existing `runAutoFix` error-return convention)

**Error Scenario 2: Without `--no-sandbox`, missing Docker still hard-refuses (regression guard)**
- **Given** no `--no-sandbox` flag and no Docker/sandbox config
- **When** `validateAutoFixBackend` runs with `--auto-fix` alone
- **Then** Story 2's resolver failure appends to `missing` and the combined usage error (exit 2) names the sandbox failure — proving this AC's bypass branch does not accidentally weaken the default-on gate for the non-bypass path

## Performance Requirements
- **Response Time:** The bypass branch must short-circuit before any `Preflight`/Docker-related I/O — the whole point is avoiding that cost when Docker is unavailable; test asserts zero calls to the resolver, not merely a fast failure from it
- **Throughput:** N/A — one bypass check per `--auto-fix` invocation

## Security Considerations
- **Authentication/Authorization:** N/A — no credential surface in the bypass branch itself
- **Input Validation:** The bypass must be scoped exactly to sandbox resolution; it must NOT also skip apply-target, validation-command, or GitHub-credential checks in the same `validateAutoFixBackend` pass — a test explicitly asserts those three still fail closed independently of `--no-sandbox`'s value

## Test Implementation Guidance
**Test Type:** UNIT (with an INTEGRATION-flavored table-driven case for the full `validateAutoFixBackend` gate)
**Test Data Requirements:** A `*cobra.Command` with `addAutoFixFlags` applied; a temp directory standing in as `repoRoot`/apply target; a fake/no-op validate command (`["true"]`/`["false"]`) so `verify.RunConfiguredValidation` behavior is deterministic without a real project build
**Mock/Stub Requirements:** A call-recording fake or package-level spy standing in for Story 2's `verify.ResolveAutoFixSandbox` (or an assertion that no Docker-shaped error/log line appears) to prove zero invocations under `--no-sandbox`; no live Docker daemon required anywhere in this AC's tests

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `validateAutoFixBackend` with `--no-sandbox=true` calls the sandbox resolver zero times, regardless of whether Docker or a `sandbox:` config is present/valid
- [ ] `runAutoFix`'s validation step executes via the direct `os/exec` path (`verify.RunConfiguredValidation`) when the backend carries no sandbox, with unchanged pass/fail/revert semantics
- [ ] `--no-sandbox=false` or absent leaves Story 2's resolver/Preflight gate fully intact (regression guard)
- [ ] Apply-target, validation-command, and GitHub-credential checks in `validateAutoFixBackend` are unaffected by `--no-sandbox`'s value in either direction

**Manual Review:**
- [ ] Code reviewed and approved

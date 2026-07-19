# Acceptance Criteria: Sandbox-Routed Command Dispatch

**Related User Story:** [01: Route Auto-Fix Validation Through the Sandbox by Default](../user-stories/01-route-autofix-validation-through-sandbox.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function/adapter in `internal/verify` (or a new sibling file, e.g. `internal/verify/sandboxvalidate.go`) | Builds a `sandbox.RunSpec` from the existing `argv`/`dir`/`timeout` inputs and calls `sandbox.Backend.Run` |
| Test Framework | `go test` + `testify` (`assert`/`require`), following `internal/verify/localvalidate_test.go` conventions | No exec mocking on the host path; a fake `sandbox.Backend` stands in for Docker |
| Key Dependencies | `internal/sandbox` (`Backend`, `RunSpec`, `RunResult`), `internal/verify` (`ValidationResult`) | No new external dependency; no Docker SDK introduced |

## Related Files
- `internal/verify/localvalidate.go` - modify: add (or extend) the sandbox-routing entry point that constructs `sandbox.RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` and dispatches to `Backend.Run` instead of `exec.CommandContext` when a backend is supplied.
- `internal/verify/localvalidate_test.go` - modify: add unit tests asserting the sandbox path is taken (fake backend records the received `RunSpec` and returns a canned `RunResult`) and that no `os/exec` child process is spawned on the host when a backend is present.
- `internal/sandbox/sandbox.go` - reference only: `Backend.Run(ctx, RunSpec) (RunResult, error)` is the interface consumed as-is; no change to this file.
- `cmd/atcr/autofix.go` - reference only: `runAutoFix` (line 252) is the sole call site whose signature/behavior this AC's routing function must remain compatible with; no functional change to this file under this specific AC (the call-site swap is covered by AC 01-03).

## Happy Path Scenarios
**Scenario 1: A configured sandbox backend routes the validation command into the container**
- **Given** an `autoFixBackend`-equivalent caller holds a non-nil `sandbox.Backend` (a fake in tests, `*sandbox.DockerBackend` in production) and a resolved `validateArgv` (e.g. `["go", "build", "./..."]`), `applyTarget` directory, and `validateTimeout`
- **When** the sandbox-routing validation function is invoked with that backend, argv, dir, and timeout
- **Then** it builds `sandbox.RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` and calls `Backend.Run(ctx, spec)` exactly once, with no `exec.CommandContext` invocation on the host for the validation command itself

**Scenario 2: SnapshotDir is passed through byte-for-byte as the absolute apply target**
- **Given** `dir` is the already-resolved absolute `applyTarget` path (per Story context, always the repo root)
- **When** the `RunSpec` is constructed
- **Then** `RunSpec.SnapshotDir` equals `dir` exactly, satisfying `sandbox.RunSpec.validate()`'s absolute-path and no-colon requirements without any additional path manipulation in this adapter

**Scenario 3: Timeout value is forwarded unchanged**
- **Given** a caller-resolved `timeout` (e.g. `be.validateTimeout`, non-zero)
- **When** the `RunSpec` is constructed
- **Then** `RunSpec.Timeout` equals `timeout` exactly; a zero timeout is not silently defaulted inside this adapter (the existing `RunConfiguredValidation` zero-timeout default at `internal/verify/localvalidate.go:81-83` remains the sole place that substitutes `defaultValidationTimeout`, so behavior is identical regardless of execution path)

## Edge Cases
**Edge Case 1: Empty argv is rejected before any sandbox call is attempted**
- **Given** `argv` is empty (`len(argv) == 0`)
- **Given** a sandbox backend is supplied
- **When** the sandbox-routing function is invoked
- **Then** it returns the same `StartError`-carrying `ValidationResult` and error that `RunConfiguredValidation` already returns for empty argv (`"auto-fix validation command not found or not executable: no command configured"`), and `Backend.Run` is never called

**Edge Case 2: A `Script`-only `RunSpec` is never produced**
- **Given** the validation path always originates from an argv (`RunConfiguredValidation`'s `argv []string` parameter), never a shell script string
- **When** the `RunSpec` is constructed
- **Then** only `RunSpec.Command` is populated and `RunSpec.Script` is left empty, satisfying `RunSpec.validate()`'s "exactly one of Command or Script" invariant deterministically (no ambiguity or accidental double-set)

**Edge Case 3: `dir` does not exist**
- **Given** `dir` points at a nonexistent path
- **When** the sandbox-routing function is invoked
- **Then** the existing pre-flight `os.Stat(dir)` check (`internal/verify/localvalidate.go:93-98`) still runs before any `Backend.Run` call and produces the same `StartError` ("validation working directory does not exist: ...") regardless of whether a sandbox backend is configured

## Error Conditions
**Error Scenario 1: `RunSpec.validate()` rejects a malformed spec (e.g. `SnapshotDir` not absolute)**
- Error message: propagated from `sandbox: RunSpec.SnapshotDir must be absolute, got %q` (returned by `Backend.Run` before any container spawn, per `internal/sandbox/sandbox.go:58-59`)
- HTTP status / error code: N/A (CLI tool) — surfaces as a Go `error` from `Backend.Run`, mapped to `ValidationResult.StartError` per AC 01-02

**Error Scenario 2: `Backend.Run` itself returns a Go error (backend fault, not a program exit)**
- Error message: whatever the backend reports (e.g. Docker daemon unreachable, spawn failure) — this AC only asserts the error is propagated out of the dispatch call unmodified, not swallowed or logged-and-ignored
- HTTP status / error code: N/A — the translation into `ValidationResult.StartError` is AC 01-02's responsibility; this AC only verifies the error reaches the translation boundary intact

## Performance Requirements
- **Response Time:** The dispatch adapter itself adds no measurable overhead beyond one `RunSpec` struct literal construction and one interface call; all latency is intrinsic to `Backend.Run` (container start/exec/teardown), already bounded by `RunSpec.Timeout`.
- **Throughput:** No new concurrency is introduced by this AC; the existing single-validation-per-`runAutoFix`-invocation shape is preserved (no fan-out, no additional goroutines).

## Security Considerations
- **Authentication/Authorization:** N/A — no credentials are introduced or consumed at this layer; the sandbox backend's own hardening (network none, read-only rootfs, capability drop, non-root) is inherited unchanged from `internal/sandbox`.
- **Input Validation:** `argv` is passed through as an explicit argument list (never a shell string) into `RunSpec.Command`, preserving the existing no-shell-interpolation guarantee documented in `RunConfiguredValidation`'s doc comment; `dir` must remain an absolute, colon-free path so `RunSpec.validate()`'s mount-injection guard (`internal/sandbox/sandbox.go:61-63`) continues to apply unchanged.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** A fake `sandbox.Backend` implementation (`Name`, `Preflight`, `Run`) that records the `RunSpec` it received and returns a caller-configured `RunResult`/error; short-lived argv fixtures (`["true"]`, `["false"]`) are not needed at this layer since the fake backend never actually execs anything.
**Mock/Stub Requirements:** Mock only `sandbox.Backend.Run` (via the fake); do not mock `os/exec` — this AC's tests assert the sandbox path is taken *instead of* `os/exec`, so no host process should be spawned for the validation command when a backend is present.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] A fake-backend unit test proves `RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` is constructed and passed to `Backend.Run` exactly once when a backend is supplied
- [ ] A unit test proves empty argv short-circuits before `Backend.Run` is called, returning the existing `StartError`
- [ ] A unit test proves a nonexistent `dir` short-circuits before `Backend.Run` is called, returning the existing `StartError`
- [ ] A unit test proves `RunSpec.Script` is never populated (only `Command`) for this call path

**Manual Review:**
- [ ] Code reviewed and approved

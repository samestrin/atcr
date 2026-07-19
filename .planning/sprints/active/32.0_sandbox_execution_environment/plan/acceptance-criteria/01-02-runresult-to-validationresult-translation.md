# Acceptance Criteria: RunResult-to-ValidationResult Translation

**Related User Story:** [01: Route Auto-Fix Validation Through the Sandbox by Default](../user-stories/01-route-autofix-validation-through-sandbox.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go pure-function adapter (e.g. `translateRunResult(sandbox.RunResult, error) verify.ValidationResult` or equivalent inline mapping) | No I/O; deterministic struct-to-struct translation only |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Table-driven tests covering each `RunResult`/error combination |
| Key Dependencies | `internal/sandbox.RunResult` (`Command`, `ExitCode`, `Output`, `TimedOut`), `internal/verify.ValidationResult` (`ExitCode`, `Stdout`, `Stderr`, `Duration`, `TimedOut`, `StartError`, `StdoutTruncated`, `StderrTruncated`) | Fields not sourced from `RunResult` (`Stderr`, `StdoutTruncated`, `StderrTruncated`) are explicitly left at zero value, not fabricated |

## Related Files
- `internal/verify/localvalidate.go` - modify: add the `RunResult` → `ValidationResult` translation logic adjacent to (or reusing) the `ValidationResult` type and its `Passed()` method, so both host and sandbox paths share the same pass/fail contract.
- `internal/verify/localvalidate_test.go` - modify: add table-driven unit tests covering exit 0, non-zero exit, `TimedOut: true`, and a `Run`-returned Go error, each asserting the resulting `ValidationResult.Passed()` matches expectation.
- `internal/sandbox/sandbox.go` - reference only: `RunResult` field semantics (`ExitCode` is "never a Go error" for a non-zero program exit; `TimedOut` marks a killed run; a non-nil `error` from `Run` is reserved for backend faults) are the source-of-truth contract this translation must honor.
- `internal/sandbox/docker.go` - reference only: `DockerBackend.Run` (lines 212-265) is the concrete backend showing exactly which conditions produce `TimedOut`, a non-zero `ExitCode`, or a returned error (Docker exit 125-127/128+N) — this AC's test fixtures should mirror those three outcome classes.

### Related Files (from codebase-discovery.json)

- `internal/verify/localvalidate.go:53-62` — update: add the `sandbox.RunResult` → `ValidationResult` translation adjacent to the `ValidationResult` type and its `Passed()` method, closing the discovery integration gap "No adapter between sandbox.RunResult and verify.ValidationResult".
- `internal/verify/localvalidate_test.go` — update: table-driven translation tests (exit 0, non-zero exit, `TimedOut`/`124`, `Run`-returned Go error) pinning `Passed()` semantics for each class.

## Happy Path Scenarios
**Scenario 1: Clean pass — exit 0, no timeout, no error**
- **Given** `Backend.Run` returns `RunResult{ExitCode: 0, Output: "build ok", TimedOut: false}` and a nil error
- **When** the result is translated
- **Then** the resulting `ValidationResult` has `ExitCode == 0`, `Stdout == "build ok"`, `TimedOut == false`, `StartError == nil`, and `Passed() == true`

**Scenario 2: Validation ran and failed — non-zero exit, no timeout, no error**
- **Given** `Backend.Run` returns `RunResult{ExitCode: 1, Output: "build failed: ...", TimedOut: false}` and a nil error
- **When** the result is translated
- **Then** the resulting `ValidationResult` has `ExitCode == 1`, `Stdout == "build failed: ..."`, `StartError == nil`, `TimedOut == false`, and `Passed() == false` — matching `runAutoFix`'s `!res.Passed()` ("validation failed") branch, not its `verr != nil` ("cannot validate") branch

**Scenario 3: Combined output is routed into Stdout only, Stderr left empty**
- **Given** `RunResult.Output` contains interleaved stdout+stderr content from the sandboxed run
- **When** the result is translated
- **Then** `ValidationResult.Stdout` receives the full `RunResult.Output` content and `ValidationResult.Stderr` is left as the empty string (the stream-collapse is a documented, deliberate loss per the story's risk analysis — not a fabricated split)

## Edge Cases
**Edge Case 1: Timeout is mapped directly, and the conventional exit code 124 is not double-reported**
- **Given** `Backend.Run` returns `RunResult{ExitCode: 124, Output: "...", TimedOut: true}` and a nil error (matching `sandbox.timeoutExitCode` used by `DockerBackend.Run` on its deadline path, `internal/sandbox/docker.go:225-227`)
- **When** the result is translated
- **Then** `ValidationResult.TimedOut == true` and `ValidationResult.ExitCode` is NOT surfaced as a fabricated/misleading non-timeout failure code — i.e., the translation treats `TimedOut == true` as authoritative and does not require callers to separately interpret `ExitCode == 124` as a distinct "exited with 124" program failure; `Passed() == false` via the `!TimedOut` clause of `Passed()`, consistent with `RunConfiguredValidation`'s own timeout handling (`internal/verify/localvalidate.go:127-137`, which returns `TimedOut: true` with `ExitCode` left at its zero value)

**Edge Case 2: A Go error from `Run` maps to StartError, matching the "cannot validate" branch**
- **Given** `Backend.Run` returns a non-nil error (e.g. Docker daemon unreachable, or a runtime fault such as exit 125-127 reported per `internal/sandbox/docker.go:239-262`) alongside a zero-value or partial `RunResult`
- **When** the result is translated
- **Then** the resulting `ValidationResult.StartError` is non-nil (wrapping or equal to the `Run` error) and the translation function itself also returns a non-nil error, so the call site's `verr != nil` check (`cmd/atcr/autofix.go:253`) fires — the "cannot even validate" branch — and NOT the `!res.Passed()` branch

**Edge Case 3: Truncated output fields are left false, not guessed**
- **Given** `sandbox.RunResult` carries no per-stream truncation signal (only a single combined `Output` string, already truncated to the backend's byte budget per `internal/sandbox/docker.go:215`)
- **When** the result is translated
- **Then** `ValidationResult.StdoutTruncated` and `ValidationResult.StderrTruncated` are both left `false` (the sandbox path does not claim knowledge it does not have); this is a documented, acceptable behavior difference from the host path's precise truncation flags, not a defect

**Edge Case 4: Duration is populated from measured wall-clock time**
- **Given** `sandbox.RunResult` has no `Duration` field
- **When** the result is translated
- **Then** the adapter measures wall-clock duration around the `Backend.Run` call itself and sets `ValidationResult.Duration` from that measurement (never left at zero), preserving parity with the host `os/exec` path which already populates `Duration`; covered by a test asserting a non-zero `Duration` reflecting the measured elapsed time

## Error Conditions
**Error Scenario 1: Docker runtime fault (exit 125-127) is never misrouted into a "validation failed" result**
- Error message: e.g. `"docker run: runtime error (exit 125): ..."` propagated from `Backend.Run`
- HTTP status / error code: N/A — surfaces as `ValidationResult.StartError` plus a non-nil return error, driving `runAutoFix`'s `verr != nil` revert-and-report path with the "cannot run validation" wording, never the "local validation failed (exit N)" wording

**Error Scenario 2: Signal-killed container (exit 128+N, e.g. 137 OOM-kill) is never misrouted into a "validation failed" result**
- Error message: e.g. `"docker run: container killed by signal 9 (OOM or daemon kill, exit 137): ..."`
- HTTP status / error code: N/A — same routing requirement as Error Scenario 1: this must land on `StartError`, not on a fabricated non-zero `ExitCode`

## Performance Requirements
- **Response Time:** Translation is an in-memory struct mapping with no I/O; it must add no measurable latency (sub-millisecond) beyond the `Backend.Run` call it wraps.
- **Throughput:** N/A — single-result, non-concurrent translation per validation run.

## Security Considerations
- **Authentication/Authorization:** N/A — no credentials handled at this layer.
- **Input Validation:** `RunResult.Output` (potentially attacker-influenced content from an LLM-generated build/test script) is stored as raw bytes into `ValidationResult.Stdout` without interpretation, mirroring `RunConfiguredValidation`'s existing "no stdout/stderr content heuristics" contract (`internal/verify/localvalidate.go:19-21`) — the translation must not parse, execute, or branch on `Output` content, only forward it.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Table-driven cases for: (1) exit 0 pass, (2) exit 1 fail, (3) `TimedOut: true` with `ExitCode: 124`, (4) `Run` returning a non-nil error with a zero-value `RunResult`, (5) `Run` returning a non-nil error alongside a partially-populated `RunResult` (mirroring Docker's runtime-fault path, which returns both a `res` and an error).
**Mock/Stub Requirements:** None beyond constructing `sandbox.RunResult` values directly and calling the pure translation function — no backend, no exec mocking needed since this AC tests only the mapping logic.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] Table-driven test proves exit-0/no-timeout/no-error maps to `Passed() == true`
- [ ] Table-driven test proves non-zero exit/no-timeout/no-error maps to `Passed() == false` with `StartError == nil` (the "validation failed" branch)
- [ ] Table-driven test proves `TimedOut: true` maps to `ValidationResult.TimedOut == true`, `Passed() == false`, without fabricating a competing failure signal from `ExitCode: 124`
- [ ] Table-driven test proves a non-nil error from `Run` maps to a non-nil `ValidationResult.StartError` and a non-nil returned error (the "cannot validate" branch), covering both a zero-value and a partially-populated `RunResult` alongside that error
- [ ] Test proves `RunResult.Output` lands in `ValidationResult.Stdout` with `Stderr` left empty, with a comment/doc note explaining the stream-collapse

**Manual Review:**
- [ ] Code reviewed and approved

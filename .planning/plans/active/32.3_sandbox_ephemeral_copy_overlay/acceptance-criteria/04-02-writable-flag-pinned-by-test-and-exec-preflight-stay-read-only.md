# Acceptance Criteria: `Writable` Flag Is Pinned by Test, `--exec`/Preflight Stay Read-Only

**Related User Story:** [04: `--auto-fix` Opts Into the Writable Overlay](../user-stories/04-auto-fix-opts-into-writable-overlay.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go unit test assertion + control-group regression pin | `internal/verify/sandboxvalidate_test.go`; no production code beyond AC 04-01's one-line change |
| Test Framework | `go test` + `testify/assert` + `fakeSandboxBackend` (`gotSpec` recording) | Same fake used by `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` |
| Key Dependencies | `internal/sandbox.RunSpec.Writable` (Story 1), `internal/tools/exec_tools.go` (`runTestsHandler`/`runScriptHandler`), `internal/verify/autofix_exec.go` (`ResolveAutoFixSandbox` -> `backend.Preflight`) | Control-group files must remain textually unchanged |

## Related Files
- `internal/verify/sandboxvalidate_test.go` - modify: `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` (line 58) gains `assert.True(t, fb.gotSpec.Writable, "auto-fix validation must request the writable overlay")` alongside its existing Command/SnapshotDir/Timeout/Script assertions (lines 68-71)
- `internal/tools/exec_tools.go` - control group, must NOT change: `runTestsHandler`'s `d.runInSandbox(ctx, sandbox.RunSpec{Command: cmd, SnapshotDir: d.root, Timeout: d.execTimeout})` (line 178) and `runScriptHandler`'s `d.runInSandbox(ctx, sandbox.RunSpec{Script: a.Content, SnapshotDir: d.root, Timeout: timeout})` (line 215) — neither sets `Writable`, both stay at the zero value `false`
- `internal/verify/autofix_exec.go` - control group, must NOT change: `ResolveAutoFixSandbox` (line 65) calls `backend.Preflight(ctx)` (line 92), whose trivial-run `RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir}` (`internal/sandbox/docker.go:308`) never sets `Writable` either

## Happy Path Scenarios
**Scenario 1: The pinning assertion catches a future accidental removal of `Writable: true`**
- **Given** `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` now asserts `fb.gotSpec.Writable == true`
- **When** a future refactor of `sandboxvalidate.go` accidentally drops `Writable: true` from the `RunSpec` literal
- **Then** this test fails immediately with the assertion message, giving CI-level signal before the regression ships and the `EROFS` false-negative silently reintroduces itself

**Scenario 2: `--exec`'s call sites keep defaulting to `Writable: false` with zero code changes**
- **Given** `runTestsHandler` and `runScriptHandler` in `exec_tools.go` never set `Writable` on their `RunSpec` literals
- **When** this story's change lands (touching only `sandboxvalidate.go` and `sandboxvalidate_test.go`)
- **Then** `git diff` shows `exec_tools.go` and `autofix_exec.go` as byte-identical to their pre-change state, and both call sites continue to construct a `RunSpec` whose `Writable` field is the Go zero value `false`, preserving `--exec`'s documented hard read-only-`/work` guarantee

## Edge Cases
**Edge Case 1: `ResolveAutoFixSandbox`'s Preflight call is a separate control group from `--exec`**
- **Given** `ResolveAutoFixSandbox` calls `backend.Preflight(ctx)`, which internally builds `RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir}` in `internal/sandbox/docker.go` (not in `verify` package code touched by this story)
- **When** this story's `Writable: true` change lands in `RunSandboxedValidation`
- **Then** the Preflight trivial-run spec is unaffected — it is a distinct code path in `internal/sandbox/docker.go` that this story does not modify — and continues to default `Writable` to `false`, proving the writable-mount opt-in does not leak into the preflight check that runs before every sandboxed `--auto-fix`/`--exec` session

**Edge Case 2: The new assertion sits alongside, not instead of, the existing `Script`-empty assertion**
- **Given** the existing assertion `assert.Empty(t, fb.gotSpec.Script, ...)` (line 71) pins the exactly-one Command/Script invariant
- **When** the new `Writable` assertion is added
- **Then** both assertions coexist in the same test function and both must pass — the new assertion does not replace or weaken the pinned `Script`-empty invariant

## Error Conditions
**Error Scenario 1: Test failure output on regression**
- Error message: `"auto-fix validation must request the writable overlay"` (the custom `testify` assertion message on `fb.gotSpec.Writable`)
- HTTP status / error code: N/A — `go test` non-zero exit / failed test case, surfaced through CI, not a runtime HTTP error

## Performance Requirements
- **Response Time:** N/A — no production runtime path affected; this is a test-only addition plus verification that two other files are untouched
- **Throughput:** N/A

## Security Considerations
- **Authentication/Authorization:** N/A
- **Input Validation:** This AC is the regression guard for `--exec`'s read-only-`/work` guarantee: by pinning `--auto-fix`'s opt-in (`Writable: true`) with an explicit test assertion and confirming `--exec`'s two call sites plus the Preflight trivial-run remain unchanged, it ensures the container-isolation boundary for LLM-authored `--exec` commands is not weakened as a side effect of unlocking `--auto-fix`'s writable overlay.

## Test Implementation Guidance
**Test Type:** UNIT (with a manual/reviewer-verified `git diff` check as a companion control-group gate)
**Test Data Requirements:** No new fixtures — reuses the existing `dir := t.TempDir()`, `argv := []string{"go", "build", "./..."}`, and `timeout := 90 * time.Second` already set up in `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec`.
**Mock/Stub Requirements:** `fakeSandboxBackend.gotSpec` (already records the full `RunSpec` it received) — no new mock needed, only a new assertion against the existing recorded field.

## Definition of Done

**Auto-Verified:**
- [ ] All tests passing
- [ ] No linting errors
- [ ] Build succeeds

**Story-Specific:**
- [ ] `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` asserts `fb.gotSpec.Writable == true`
- [ ] `git diff` confirms `internal/tools/exec_tools.go` is unchanged by this story
- [ ] `git diff` confirms `internal/verify/autofix_exec.go` is unchanged by this story
- [ ] The existing `assert.Empty(t, fb.gotSpec.Script, ...)` assertion (line 71) still passes alongside the new `Writable` assertion

**Manual Review:**
- [ ] Code reviewed and approved

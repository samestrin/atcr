# Acceptance Criteria: fakeDocker-Based Proof a Script Can Write Under /work

**Related User Story:** [05: Regression Proof and Documentation Parity](../user-stories/05-regression-proof-and-docs-parity.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go test in `internal/sandbox/sandbox_test.go`, reusing the `writeFakeDocker` POSIX shell shim | Functional proof, not just argv/string matching |
| Test Framework | `go test` + `testify` (`assert`/`require`), POSIX `/bin/sh` shim | Skips on Windows, mirroring existing `writeFakeDocker` callers |
| Key Dependencies | `writeFakeDocker` (`sandbox_test.go:24`) | Existing helper, reused with a new script body — no new scaffolding |

## Related Files
- `internal/sandbox/sandbox_test.go` - modify: add a new test function using `writeFakeDocker` (line 24) keyed on the recorded argv's `--tmpfs /work` flag and `cp -a`/`exec "$@"` wrap, with a shim script body that performs an actual file write to prove the writable-mount mechanism functions
- `internal/sandbox/docker.go` - reference only: `DockerBackend.Run` (line 161), the code path that invokes the fake `docker` binary via `exec.CommandContext` and whose behavior (argv construction, stdin piping) this test exercises end-to-end without a real daemon

## Happy Path Scenarios
**Scenario 1: Fake docker shim performs an observable write and the test asserts it**
- **Given** a `writeFakeDocker` shim body that recognizes the `Writable:true` invocation shape (presence of `--tmpfs /work` and the `cp -a`/`exec "$@"` wrap in its received argv) and, when recognized, writes a marker file to a path supplied via an env var or a `t.TempDir()`-backed path baked into the shim body
- **When** `DockerBackend.Run` is called with a `Writable:true` `RunSpec` (Command or Script mode) pointed at the fake docker path
- **Then** the test reads back the marker file after `Run` returns and asserts its existence and expected content, proving the writable-mount code path is reachable and functional, not just present in the argv string

**Scenario 2: The write-proof test covers at least one of Command or Script mode**
- **Given** the constraint that the fakeDocker shim only sees the argv/stdin `Run` actually constructs
- **When** the test is written against either Command-mode or Script-mode `Writable:true` (the story does not require both to be covered by this specific test, since AC 05-01 already covers both modes' argv/stdin shape)
- **Then** the chosen mode's marker-file write is asserted as proof the mechanism works for a real `cp -a`-then-execute sequence

## Edge Cases
**Edge Case 1: Non-POSIX CI runners skip cleanly, not silently pass**
- **Given** `writeFakeDocker` already calls `t.Skip("fake-docker shell shim is POSIX-only")` on `runtime.GOOS == "windows"` (`sandbox_test.go:26-28`)
- **When** this AC's new test runs on a Windows runner
- **Then** it is skipped via the same helper (not duplicated skip logic), and the skip is visible in `go test -v` output as `SKIP`, never reported as a pass

**Edge Case 2: The proof asserts the observable write, not just that the shim ran**
- **Given** a shim that could technically exit 0 without performing the write (a coding mistake in the shim body)
- **When** the test's marker-file assertion runs
- **Then** the test fails loudly (`require.NoError`/`assert.FileExists` or equivalent) if the marker file is absent, rather than only checking `Run`'s returned exit code — this keeps the test a genuine functional proof rather than a disguised argv-shape check

## Error Conditions
**Error Scenario 1: Shim write failure surfaces as a test failure, not a silent pass**
- If the fake docker shim's write step fails (e.g. permission error in `t.TempDir()`), the shim should exit non-zero so `Run`'s returned `RunResult`/error reflects the failure, and the test asserts on that failure explicitly rather than only checking file existence after an unchecked `Run` call.
- Error message: the test failure message must name what was expected (`"expected file to exist under /work after Writable:true run"`) so a future regression is immediately diagnosable from `go test` output.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** The fakeDocker shim executes as a lightweight shell script (no real container startup), so this test must run in well under a second, consistent with the existing `writeFakeDocker`-based tests in the package.
- **Throughput:** Not applicable — single test invocation, no concurrency requirement.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go test, no external auth surface.
- **Input Validation:** The shim script body must not itself become an injection vector into the test suite (e.g. it should not `eval` unsanitized input) — it is a static, hardcoded shell body written by the test author, matching the pattern of every other `writeFakeDocker` caller in the package.

## Test Implementation Guidance
**Test Type:** UNIT (functional simulation, no real daemon)
**Test Data Requirements:** A `writeFakeDocker` shim body string that inspects `$@` for the `--tmpfs /work` and `cp -a`/`exec "$@"` markers, then writes a known file to a known path; a `RunSpec` with `Writable: true` and a `SnapshotDir` pointing at a `t.TempDir()`.
**Mock/Stub Requirements:** `writeFakeDocker` (existing helper) is the only mock/stub needed — no real Docker daemon, no network.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] New test reuses `writeFakeDocker` (no new scaffolding/helper introduced)
- [ ] Test asserts an observable file write (existence + content), not just argv shape or exit code
- [ ] Test skips cleanly (via the existing `t.Skip` in `writeFakeDocker`) on Windows, matching every other caller's skip behavior
- [ ] Test is additive — no edits to `TestDockerRunArgs_HardeningFlagsPresent`, `TestDockerRunArgs_ScriptUsesStdinShell`, or any other pre-existing test in the file

**Manual Review:**
- [ ] Code reviewed and approved

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

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker_test.go` — extend (primary home per `codebase-discovery.json` → `files_to_modify`: "Add a fakeDocker-based test proving a mock validation script can write a file under /work when Writable:true"; same `sandbox` package as the shim, so `writeFakeDocker` is directly callable)
- `internal/sandbox/sandbox_test.go:24` (`writeFakeDocker`) — reference-only helper definition site; extend only if the new test is placed in this file instead of `docker_test.go` (either location is acceptable — one package, shared helpers)
- `internal/sandbox/docker.go:161` (`DockerBackend.Run`) — reference-only (code path under test; untouched by this story)

## Happy Path Scenarios
**Scenario 1: Fake docker shim performs an observable write and the test asserts it**
- **Given** a `writeFakeDocker` shim body that recognizes the `Writable:true` invocation shape (presence of `--tmpfs /work` and the `cp -a`/`exec "$@"` wrap in its received argv) and, when recognized, writes a marker file to a path supplied via an env var or a `t.TempDir()`-backed path baked into the shim body
- **When** `DockerBackend.Run` is called with a `Writable:true` `RunSpec` (Command or Script mode) pointed at the fake docker path
- **Then** the test reads back the marker file after `Run` returns and asserts its existence and expected content, proving the writable-mount code path is reachable and functional, not just present in the argv string

**Scenario 2: The write-proof test covers Command mode (mandatory), Script mode optional**
- **Given** the constraint that the fakeDocker shim only sees the argv/stdin `Run` actually constructs
- **When** the test is written against Command-mode `Writable:true` — mandatory, because `--auto-fix`'s `RunSandboxedValidation` constructs `RunSpec{Command: argv}` exclusively (pinned invariant, `internal/verify/sandboxvalidate_test.go:71`), so Command mode IS the `--auto-fix` code path this sprint exists to unlock; a write-proof that covered only Script mode would never exercise the mode the objective depends on
- **Then** the Command-mode marker-file write is asserted as proof the mechanism works for a real `cp -a`-then-execute sequence
- **Note:** `codebase-discovery.json` → `files_to_modify` names `internal/sandbox/docker_test.go` as this test's home and contemplates both modes ("for both Command and Script modes"); Command-mode coverage is the story's mandatory minimum bar (AC 05-01 already pins both modes' argv/stdin shape), and a table-driven sub-test that additionally covers Script mode is preferred where the shim body supports it cheaply

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
**Naming Convention:** `TestDockerBackend_<Scenario>` (e.g. `TestDockerBackend_WritableRunCanWriteUnderWork`) — this is a `DockerBackend.Run`-level functional test, per `codebase-discovery.json` → `test_patterns.naming_convention`.
**Skip Parity:** the Windows skip must come from `writeFakeDocker` itself (`t.Skip("fake-docker shell shim is POSIX-only")`, `sandbox_test.go:26-28`) — mirror the existing skip behavior and reason exactly; do not add a second, divergent skip in the new test.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/sandbox/...`)
- [x] No linting errors (`go vet ./...` / project lint gate)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] New test reuses `writeFakeDocker` (no new scaffolding/helper introduced)
- [x] Test asserts an observable file write (existence + content), not just argv shape or exit code
- [x] Command-mode `Writable:true` is covered by the write-proof (mandatory — it is `--auto-fix`'s exclusive `RunSpec` mode); Script-mode coverage is optional/additive — `TestDockerBackend_Run_WritableOverlayWriteProof` is table-driven over BOTH modes
- [x] Test skips cleanly (via the existing `t.Skip` in `writeFakeDocker`) on Windows, matching every other caller's skip behavior
- [x] Test is additive — no edits to `TestDockerRunArgs_HardeningFlagsPresent`, `TestDockerRunArgs_ScriptUsesStdinShell`, or any other pre-existing test in the file
- [x] Test lands in `internal/sandbox/docker_test.go` (per `codebase-discovery.json` → `files_to_modify`) or `sandbox_test.go` — same package either way — and follows the `TestDockerBackend_<Scenario>` naming convention

**Manual Review:**
- [ ] Code reviewed and approved

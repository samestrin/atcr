# Acceptance Criteria: Writable:false Regression Test Anchor Stays Unmodified

**Related User Story:** [05: Regression Proof and Documentation Parity](../user-stories/05-regression-proof-and-docs-parity.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go tests in `internal/sandbox/sandbox_test.go` and `internal/verify/autofix_exec_test.go` | Additive/regression-only — no edits to the two named anchor tests |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Reuses `fakeDockerRecording` (`autofix_exec_test.go:23`) and `runArgsLine` (`:43`) for the Preflight control group |
| Key Dependencies | none new | Existing helpers only |

## Related Files
- `internal/sandbox/sandbox_test.go` - modify: add a new dedicated `Writable:false` case (full-string argv comparison against the pre-story literal) or a standalone byte-for-byte snapshot test, without touching `TestDockerRunArgs_HardeningFlagsPresent` (line 35) or its assertion at line 55 (`assert.Contains(t, joined, "/tmp/snap:/work:ro", ...)`)
- `internal/verify/autofix_exec_test.go` - reference only (not modified): `TestResolveAutoFixSandbox_BuildsAndPreflights` (line 56) uses `fakeDockerRecording` (line 23) and `runArgsLine` (line 43); Preflight always runs with `Writable:false`, forming the second control group proving the writable-mount branch never leaks into the preflight path — this AC's Definition of Done requires this test to keep passing unmodified
- `internal/sandbox/docker.go` - reference only: `Preflight` (line 281-316), whose trivial-run call `dockerRunArgs(b.cfg, RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir})` (line 308) never sets `Writable`, making it the production-code control group this AC's tests confirm stays `Writable:false`

### Related Files (from codebase-discovery.json)
- `internal/sandbox/sandbox_test.go:35,55` (`TestDockerRunArgs_HardeningFlagsPresent` and its `/tmp/snap:/work:ro` assertion) — extend (new additive case or sibling function only; the anchor itself is byte-untouched)
- `internal/verify/autofix_exec_test.go:23,43,56` (`fakeDockerRecording`, `runArgsLine`, `TestResolveAutoFixSandbox_BuildsAndPreflights`) — reference-only (Preflight control group; must pass unmodified)
- `internal/tools/exec_tools.go:160` (`runTestsHandler`/`runScriptHandler`/`runInSandbox`) — reference-only (the two `--exec` call sites whose zero-value `Writable:false` this AC protects)
- `internal/sandbox/docker.go:281-316` (`Preflight`) — reference-only (production-code control group; untouched by this story)

## Happy Path Scenarios
**Scenario 1: New byte-identical regression case for Writable:false**
- **Given** the same `RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}` fixture used by `TestDockerRunArgs_HardeningFlagsPresent`, with `Writable` left at its zero value (`false`)
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** a new test asserts the full joined argv string (or the argv slice) is byte-for-byte identical to a captured pre-story literal, added as either a new table row in the existing table-driven structure or a standalone snapshot-comparison test function

**Scenario 2: TestDockerRunArgs_HardeningFlagsPresent passes with zero edits**
- **Given** the pre-existing test at `sandbox_test.go:35` with its assertion at `:55`
- **When** `go test -run TestDockerRunArgs_HardeningFlagsPresent ./internal/sandbox/...` runs after this story's changes
- **Then** it passes, and `git diff` on `sandbox_test.go` shows zero lines changed inside that specific test function's body

**Scenario 3: Preflight's control-group argv shows no Writable:true leakage**
- **Given** `TestResolveAutoFixSandbox_BuildsAndPreflights` (`autofix_exec_test.go:56`) drives `ResolveAutoFixSandbox` → `Preflight` → `dockerRunArgs` through the `fakeDockerRecording` shim
- **When** the recorded `docker run` invocation is inspected via `runArgsLine`
- **Then** the invocation contains the existing read-only mount shape and no `/src:ro` mount or `--tmpfs /work` flag, confirming Preflight's trivial container never opts into the writable branch

**Scenario 4: Script-mode Writable:false argv and stdin body are byte-identical**
- **Given** the same Script-mode fixture used by `TestDockerRunArgs_ScriptUsesStdinShell` (`RunSpec{Script: "echo hi\nexit 3\n", SnapshotDir: "/tmp/snap"}`), with `Writable` left at its zero value (`false`)
- **When** `dockerRunArgs` builds the argv and `Run` constructs the stdin reader
- **Then** the argv is byte-identical to the pre-story Script-mode shape (`-i <image> /bin/sh -s`, `/tmp/snap:/work:ro`, no `/src:ro`, no `--tmpfs /work`) and the stdin body is exactly the original script text — no `cp -a /src/. /work/ && cd /work` setup line is prepended on the false path, proving Story 3's Script-mode injection is strictly conditional on `Writable:true`

## Edge Cases
**Edge Case 1: Isolated pre/post comparison of the anchor test**
- **Given** the risk (identified in the story's Potential Risks table) that a new table-driven case could accidentally require touching shared setup code that shifts the anchor test's expected output
- **When** `go test -run TestDockerRunArgs_HardeningFlagsPresent` is run in isolation before and after this AC's changes land
- **Then** the two runs show identical pass/fail status and identical assertion behavior — any diff is treated as a story failure, not an acceptable side effect

**Edge Case 2: TestResolveAutoFixSandbox_FullFieldOverrideAppliedBeforePreflight is unaffected**
- **Given** the sibling test at `autofix_exec_test.go:70` also exercises `ResolveAutoFixSandbox` → `Preflight` with field overrides (Memory, CPUs, PidsLimit, Image)
- **When** this AC's regression work lands
- **Then** that test's existing assertions (`--memory 256m`, `--cpus 0.5`, `--pids-limit 128`, `custom-image:9`) continue to pass unmodified, since Preflight's `RunSpec` construction is untouched by this story

## Error Conditions
**Error Scenario 1: A regression is caught as a test failure, not a runtime error**
- If a future change to the `Writable:false` branch (introduced by Stories 2-3) accidentally alters the mount target, drops `:ro`, or leaks a `Writable:true`-only flag, this AC's byte-identical comparison and `TestDockerRunArgs_HardeningFlagsPresent` both fail at `go test` time — there is no runtime error surface to observe since the container would still start, just with a weakened guarantee.
- Error message: the new regression test's failure message should name the specific literal expected (e.g. `"Writable:false argv must match pre-story literal exactly"`), so a diff is immediately actionable.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

**Error Scenario 2: Preflight leakage would be a security-relevant regression**
- If `Writable:true` behavior ever leaked into `Preflight`'s trivial-run construction, the preflight container (and, by extension, any misconfigured production code reusing that pattern) would gain an unintended writable `/work` mount.
- Error message: not applicable — this is caught by the absence of `/src:ro`/`--tmpfs /work` in the recorded `runArgsLine` output, an assertion failure rather than a runtime error message.
- HTTP status / error code: not applicable.

## Performance Requirements
- **Response Time:** Not applicable — pure-function and shim-based unit tests; execution time remains sub-second per test, consistent with the existing suite.
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go test, no external auth surface.
- **Input Validation:** This AC's tests are the CI-enforced proof that `--exec`'s hard read-only-`/work` guarantee (documented in `docs/execution.md:51-62` and the `internal/sandbox` package doc) has zero regression; no new input-validation logic is introduced, only assertions against existing validated behavior.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Reuse the exact `RunSpec` fixture from `TestDockerRunArgs_HardeningFlagsPresent`; capture the pre-story argv literal once (e.g. by running the test before Stories 2-3 land, or by deriving it from the current `dockerRunArgs` false-branch source) to use as the golden comparison value.
**Mock/Stub Requirements:** `fakeDockerRecording` and `runArgsLine` (existing helpers in `internal/verify/autofix_exec_test.go`) for the Preflight control-group assertion; no new mocks.
**Naming Convention:** `TestDockerRunArgs_<Scenario>` for the new pure-builder regression case (e.g. `TestDockerRunArgs_WritableFalseByteIdentical`), per `codebase-discovery.json` → `test_patterns.naming_convention`; assertions stay daemon-free at the argv/stdin level, matching the package's existing pattern.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/... ./internal/verify/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] New byte-identical `Writable:false` regression case added (table row or standalone snapshot test)
- [ ] `TestDockerRunArgs_HardeningFlagsPresent` (`sandbox_test.go:35`, assertion `:55`) shows zero diff in `git diff` for this story
- [ ] `TestResolveAutoFixSandbox_BuildsAndPreflights` (`autofix_exec_test.go:56`) and `TestResolveAutoFixSandbox_FullFieldOverrideAppliedBeforePreflight` (`:70`) pass unmodified, confirming no `Writable:true` leakage into the Preflight control group
- [ ] `go test -run TestDockerRunArgs_HardeningFlagsPresent` run in isolation shows identical behavior before and after this story's changes
- [ ] New regression coverage also pins the Script-mode `Writable:false` path: argv byte-identical to the pre-story shape and the stdin script body free of any prepended `cp -a` setup line

**Manual Review:**
- [ ] Code reviewed and approved

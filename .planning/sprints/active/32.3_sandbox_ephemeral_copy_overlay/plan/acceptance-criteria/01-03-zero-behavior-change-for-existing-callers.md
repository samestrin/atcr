# Acceptance Criteria: Zero Behavior Change for Existing `--exec` Callers and Test Suite

**Related User Story:** [01: Opt-In Writable Configuration Surface](../user-stories/01-opt-in-writable-configuration-surface.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Regression guarantee across `internal/sandbox` and `internal/tools` | No new code paths; verifies additive-only change |
| Test Framework | `go test` (full existing suite, unmodified) + `go build` | `internal/sandbox/sandbox_test.go`, `internal/sandbox/docker_test.go`, `internal/tools` exec tests |
| Key Dependencies | none | Confirms Go's implicit zero-value struct-literal semantics hold |

## Related Files
- `internal/tools/exec_tools.go` - reference only (not modified): line 178 (`runTestsHandler`'s `d.runInSandbox(ctx, sandbox.RunSpec{Command: cmd, SnapshotDir: d.root, Timeout: d.execTimeout})`) and line 215 (`runScriptHandler`'s `d.runInSandbox(ctx, sandbox.RunSpec{Script: a.Content, SnapshotDir: d.root, Timeout: timeout})`) — neither call site sets `Writable`, so both continue to produce `Writable: false` after this story
- `internal/sandbox/sandbox.go` - reference only (not modified in behavior): `RunSpec.validate()` (line 43) and `dockerRunArgs`/`Run` in `internal/sandbox/docker.go` must not branch on the new fields
- `internal/sandbox/sandbox_test.go` - reference: full existing suite must pass unmodified (e.g. `TestDockerRunArgs_HardeningFlagsPresent`, `TestDockerRunArgs_WritableTempEnv`, `TestNewDockerBackend_ConcurrencyCap`)
- `internal/sandbox/docker_test.go` - reference: full existing suite must pass unmodified (e.g. `TestDockerBackendRun_RuntimeExitCodesAreBackendErrors`, `TestDockerBackend_Preflight_CatchesInvalidCPUs`)

### Related Files (from codebase-discovery.json)
- `internal/tools/exec_tools.go:178` (`runTestsHandler`) and `:215` (`runScriptHandler`) — reference-only control group: neither `RunSpec{...}` literal sets `Writable`; zero lines changed in this file
- `internal/sandbox/sandbox_test.go:35` (`TestDockerRunArgs_HardeningFlagsPresent`; `"/tmp/snap:/work:ro"` anchor at :55), `:64` (`TestDockerRunArgs_WritableTempEnv`), `:83` (`TestDockerRunArgs_ScriptUsesStdinShell`) — reference-only: must pass unmodified
- `internal/sandbox/docker_test.go:29-260` — reference-only: full file passes unmodified (exit-code classification, preflight host-cap checks, timeout kill)
- `internal/sandbox/sandbox.go:43-65` (`RunSpec.validate()`) and `internal/sandbox/docker.go:110` (`dockerRunArgs`) — reference-only: zero behavior-affecting diff
- `internal/verify/autofix_exec_test.go:23` (`fakeDockerRecording`) and `:56` (`TestResolveAutoFixSandbox_BuildsAndPreflights`) — reference-only control group: Preflight always runs `Writable:false`, so these argv-recording tests prove the default `docker run` argv is untouched

## Happy Path Scenarios
**Scenario 1: `--exec` call sites compile and run unchanged**
- **Given** the two `RunSpec{...}` literals in `internal/tools/exec_tools.go:178` and `:215`, unedited by this story
- **When** `go build ./...` runs after `Writable` and `WorkSize` are added
- **Then** the build succeeds with zero changes required at either call site, because Go zero-initializes the unset `Writable` field to `false`

**Scenario 2: Full existing sandbox test suite passes unmodified**
- **Given** `internal/sandbox/sandbox_test.go` and `internal/sandbox/docker_test.go` as they exist today, with no test file edits required by this story (beyond the additive new tests from AC 01-01 and AC 01-02)
- **When** `go test ./internal/sandbox/...` runs
- **Then** every existing test passes with zero failures and zero skips attributable to this story's change

## Edge Cases
**Edge Case 1: dockerRunArgs argv is byte-identical for existing callers**
- **Given** `dockerRunArgs(cfg, spec)` is called with a `spec` built the same way `--exec`'s two call sites build it today (no `Writable` set)
- **When** the returned argv is compared against the argv produced before this story's change
- **Then** the argv is byte-for-byte identical — still `-v <SnapshotDir>:/work:ro`, still no writable `/work` mount, proving the new field does not alter mount construction for any existing caller

**Edge Case 2: `RunSpec.validate()` invariants are unaffected**
- **Given** the exactly-one-of-Command/Script check and the `SnapshotDir` absolute-path / no-colon injection checks in `validate()` (`internal/sandbox/sandbox.go:43-65`)
- **When** `Writable` is set to either `true` or `false` on an otherwise-identical `RunSpec`
- **Then** `validate()`'s pass/fail outcome is identical in both cases — `Writable` does not interact with or gate any existing validation branch

**Edge Case 3: Preflight's trivial-run argv is unchanged (control group)**
- **Given** `Preflight` builds its trivial-run argv via `dockerRunArgs` with `RunSpec{Command: []string{"true"}}` — `Writable` at its zero value — and the resolver tests record that exact argv (`fakeDockerRecording`, `internal/verify/autofix_exec_test.go:23,56`)
- **When** `go test ./internal/sandbox/... ./internal/verify/...` runs after this story
- **Then** the preflight tests (`internal/sandbox/docker_test.go:55-118`) and the resolver argv assertions pass unmodified, proving the new fields do not leak into the default `docker run` argv

## Error Conditions
**Error Scenario 1: A regression would surface as an existing test failure, not a new error type**
- If this story's field additions accidentally altered `dockerRunArgs`, `Run`, or `validate()` behavior, the failure signal is one or more of the existing tests (e.g. `TestDockerRunArgs_HardeningFlagsPresent`'s `assert.Contains(t, joined, "/tmp/snap:/work:ro", ...)`) failing — this AC's Definition of Done is exactly "these do not fail."
- Error message: no new error message; a violation is detected as a pre-existing assertion failing.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** No change to `--exec`'s existing per-run latency — this story adds no branching to any code path `--exec` executes.
- **Throughput:** No change — `MaxConcurrent` semaphore behavior and container spawn cost are untouched.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** This AC is itself the security control: it proves `--exec`'s hard read-only-`/work` guarantee (the Epic 11.0 containment invariant documented in `internal/sandbox/sandbox.go`'s package doc comment — "a read-only view of the snapshot") is not weakened by the new opt-in surface, since neither `--exec` call site sets `Writable` and no code reads it yet. Blast radius of a mistake here would be silently making `--exec` writable; the full existing test suite passing unmodified is the guardrail against that.
- **Documentation Guarantee Anchor:** `docs/execution.md:51-62,86-90` ("`/work` (the snapshot) is read-only; the only writable location is the `/scratch` tmpfs") and the `internal/sandbox` package doc (`sandbox.go:1-15`) are reference-only in this story — no edits — and must remain textually true after it lands; any diff that would falsify them is a bug (PRESERVE anchors in `../documentation/current-sandbox-guarantees.md`).

## Test Implementation Guidance
**Test Type:** INTEGRATION (full-package regression run) backed by UNIT tests
**Test Data Requirements:** No new fixtures — this AC is satisfied by the existing `internal/sandbox` and `internal/tools` test fixtures running unmodified, plus the additive tests from AC 01-01/01-02.
**Mock/Stub Requirements:** Reuse the existing `writeFakeDocker` shim pattern (`internal/sandbox/sandbox_test.go:24`) already used by the argv-shape assertions; no new mocks needed.
**Verification Commands:** `go build ./...`; `go test ./internal/sandbox/... ./internal/tools/... ./internal/verify/...` (green, zero new skips); `git diff --stat -- internal/tools/exec_tools.go` is empty; `git diff internal/sandbox/sandbox.go internal/sandbox/docker.go` shows only additive field/doc-comment/default lines — no body changes in `dockerRunArgs`, `Run`, `Preflight`, `NewDockerBackend`, or `validate()`.

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./...`, with particular attention to `./internal/sandbox/...` and `./internal/tools/...`)
- [x] No linting errors (`go vet ./...` / project lint gate)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] Zero lines changed in `internal/tools/exec_tools.go` (diff confirms both `RunSpec{...}` call sites at lines 178 and 215 are untouched)
- [x] `dockerRunArgs`, `Run`, and `RunSpec.validate()` have zero behavior-affecting diff (only comment/field additions elsewhere in the same files, if any)
- [x] Full pre-existing `internal/sandbox/sandbox_test.go` and `internal/sandbox/docker_test.go` suites pass with no modifications to existing test bodies
- [x] `dockerRunArgs` output for a `--exec`-shaped `RunSpec` (no `Writable` set) is unchanged from pre-story behavior
- [x] `internal/verify` preflight/resolver control-group tests (`internal/verify/autofix_exec_test.go:23,56`) pass unmodified, proving the default (`Writable:false`) argv is untouched
- [x] `docs/execution.md` and the `internal/sandbox` package doc remain unedited and textually accurate (PRESERVE anchors in `../documentation/current-sandbox-guarantees.md`)

**Manual Review:**
- [ ] Code reviewed and approved

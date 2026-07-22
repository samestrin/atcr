# Acceptance Criteria: Auto-Fix Validation Requests the Writable Overlay

**Related User Story:** [04: `--auto-fix` Opts Into the Writable Overlay](../user-stories/04-auto-fix-opts-into-writable-overlay.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go `sandbox.RunSpec` struct-literal construction | `internal/verify/sandboxvalidate.go`, Command-mode dispatch only |
| Test Framework | `go test` + `testify/assert`/`require` + `fakeSandboxBackend` | No Docker daemon; a Go-level recording fake stands in for `sandbox.Backend` |
| Key Dependencies | `internal/sandbox` (`RunSpec.Writable`, `dockerRunArgs`, `Run` — Stories 1-2), `internal/verify` (`RunSandboxedValidation`, `translateRunResult`) | `RunSpec.Writable` and the mount-branching logic must already exist (Story 1, Story 2) |

## Related Files
- `internal/verify/sandboxvalidate.go` - modify: add `Writable: true` to the `sandbox.RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` literal at lines 62-66 inside `RunSandboxedValidation`
- `internal/verify/sandboxvalidate_test.go` - modify: extend `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` (line 58) with an assertion on `fb.gotSpec.Writable`
- `internal/sandbox/docker.go` - reference only (not modified): `dockerRunArgs` (Story 2) is the consumer that branches on `spec.Writable` to mount `/src` read-only and back `/work` with a writable tmpfs

### Related Files (from codebase-discovery.json)
- `internal/verify/sandboxvalidate.go:62-66` (`RunSandboxedValidation`'s `sandbox.RunSpec` literal) — modify: add `Writable: true` only; the empty-argv/missing-dir guards (`:44-60`) and `translateRunResult` (`:100-117`) stay byte-identical
- `internal/verify/sandboxvalidate_test.go:58` (`TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec`; per-field forwarding assertions at `:68-71`; `fakeSandboxBackend` recording helper at `:33-54`) — extend: gains `assert.True(t, fb.gotSpec.Writable, ...)`; closes the discovery `integration_gaps` entry "Task 4 has no pinning test site"
- `internal/sandbox/docker.go:110` (`dockerRunArgs`) — reference-only: Story 2 owns the `spec.Writable` mount/argv branch this flag activates; not modified by this AC

## Happy Path Scenarios
**Scenario 1: A non-Go validate_command that writes into the working tree now succeeds**
- **Given** an `--auto-fix` run has a `[sandbox]` block configured and an `auto_fix.validate_command` that writes into the project directory (e.g. `npm run build` writing to `dist/`, or a synthetic script running `mkdir dist && touch dist/out`)
- **When** `RunSandboxedValidation` dispatches that argv through `backend.Run` with the `RunSpec` it now builds (`Writable: true`)
- **Then** the container mounts the snapshot read-only at `/src` and backs `/work` with a writable tmpfs (per Story 2's mechanism), the write to `dist/` succeeds instead of failing with `EROFS`, `translateRunResult` maps a clean exit 0 to `res.Passed() == true`, and the fix is accepted rather than silently discarded

**Scenario 2: Existing Go validate_command behavior is unaffected**
- **Given** an `--auto-fix` run with the default `go build ./...` / `go test ./...` validate_command
- **When** `RunSandboxedValidation` dispatches it with `Writable: true`
- **Then** the build/test still passes exactly as it did before this change — Go's toolchain already redirected caches to `/scratch`, so the added writable `/work` overlay is a superset of capability, not a behavior change, and `go build ./...` / `go test ./internal/verify/...` continue to pass with zero new failures

## Edge Cases
**Edge Case 1: Pre-flight guards still short-circuit before the writable spec is ever built**
- **Given** `argv` is empty, or `dir` does not exist on disk
- **When** `RunSandboxedValidation` is called
- **Then** the existing guards at lines 44-60 return a `StartError` immediately, exactly as before this change — no `RunSpec` (writable or otherwise) is constructed and `backend.Run` is never called (pinned by `TestRunSandboxedValidation_EmptyArgvShortCircuitsBeforeBackend` and `TestRunSandboxedValidation_MissingDirShortCircuitsBeforeBackend`, `sandboxvalidate_test.go:79` and `:88`, each asserting `fb.calls == 0`; both must pass unmodified)

**Edge Case 2: `Script` remains empty on this dispatch path**
- **Given** `RunSandboxedValidation` is a pure Command-mode caller
- **When** the `RunSpec` is constructed with `Writable: true`
- **Then** `Script` is still never populated — the field addition touches only `Writable`, leaving the exactly-one `Command`/`Script` invariant intact

**Edge Case 3: `Writable: true` does not disturb the backend's fail-closed `RunSpec.validate()` checks**
- **Given** an empty `dir` (deliberately not stat-guarded by the adapter — the documented empty-dir delegation) or a relative `dir` that exists and therefore passes the adapter's `os.Stat` guard
- **When** `RunSandboxedValidation` dispatches the spec — now carrying `Writable: true` — to a real `DockerBackend`
- **Then** the backend's `RunSpec.validate()` still rejects the empty/relative `SnapshotDir` before any container spawn (`Writable` is an independent field with no interaction with the exactly-one `Command`/`Script` invariant or the absolute-path/no-colon `SnapshotDir` checks), surfacing as `StartError` + `!res.Passed()` + a non-nil returned error with zero docker subprocess spawns — pinned end-to-end by `TestRunSandboxedValidation_EmptyDirFailsClosedThroughRealBackend` (`sandboxvalidate_test.go:227`) and `TestRunSandboxedValidation_RelativeDirFailsClosedThroughRealBackend` (`sandboxvalidate_test.go:245`), which must pass unmodified against the writable spec

## Error Conditions
**Error Scenario 1: Backend fault after the writable spec is dispatched**
- Error message: `"auto-fix sandbox validation could not run: %w"` (wraps the underlying `backend.Run` error, e.g. `"docker daemon unreachable"`)
- HTTP status / error code: N/A (library-level Go error; surfaces as a non-nil `StartError` and non-nil returned `error`, per `translateRunResult`'s existing fault-mapping — unchanged by this story)

## Performance Requirements
- **Response Time:** No measurable regression — the copy-on-run setup step (`cp -a` from `/src` into the `/work` tmpfs) is Story 2's mechanism and already accounted for in its own performance budget; this story adds zero new I/O or logic beyond the one struct field
- **Throughput:** N/A (single validation dispatch per `--auto-fix` invocation, unchanged call cadence)

## Security Considerations
- **Authentication/Authorization:** N/A — no auth surface in this change
- **Input Validation:** `argv` and `dir` continue to pass through the same pre-flight guards (empty argv, missing dir) and the backend's `RunSpec.validate()` (empty/relative `SnapshotDir`) before any container spawn; enabling `Writable: true` does not bypass or weaken any existing validation. The `/src` read-only mount + `/work` tmpfs split (Story 2) ensures the writable surface is an ephemeral copy, never the operator's real working tree, so a malicious or buggy validate_command cannot corrupt source-of-truth files on the host. The snapshot (`/src` under `Writable:true`) remains read-only for its entire lifetime and the tmpfs copy — with everything written into it — dies with the container: no host file is ever mutated, per the PRESERVE/UPDATE map in [documentation/current-sandbox-guarantees.md](../documentation/current-sandbox-guarantees.md).

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** No new fixtures — reuses the existing `t.TempDir()` working directory and argv (`{"go", "build", "./..."}`) from `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec`; the happy-path non-Go write scenario (Scenario 1) is documentation/behavior-level (validated by Story 2's own writable-mount tests plus this story's unit-level field assertion), not re-implemented as a new integration test here.
**Mock/Stub Requirements:** `fakeSandboxBackend` (already defined in `sandboxvalidate_test.go:33-54`) — a Go-level recording fake implementing `sandbox.Backend`, no Docker daemon required.
**Assertion/Naming Pattern:** Daemon-free, Go-level assertions on the recorded `RunSpec` fields inside the existing `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` (this package's established convention — argv/mount-level assertions live in `internal/sandbox`, not here). The real-backend fail-closed tests (`sandboxvalidate_test.go:227`, `:245`) and the short-circuit tests (`:79`, `:88`) must pass unmodified with the writable spec (Edge Cases 1 and 3).

## Definition of Done

**Auto-Verified:**
- [x] All tests passing
- [x] No linting errors
- [x] Build succeeds

**Story-Specific:**
- [x] `sandboxvalidate.go`'s `RunSpec` literal at lines 62-66 includes `Writable: true`
- [x] `go build ./...` succeeds with zero new failures
- [x] `go test ./internal/verify/...` passes with zero new failures
- [x] `translateRunResult` and the pre-flight guards (empty argv, missing dir) remain byte-identical to their pre-change behavior

**Manual Review:**
- [ ] Code reviewed and approved

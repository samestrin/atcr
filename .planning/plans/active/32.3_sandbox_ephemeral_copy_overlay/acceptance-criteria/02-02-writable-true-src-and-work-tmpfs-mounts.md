# Acceptance Criteria: Writable:true Mounts /src Read-Only and /work as a tmpfs

**Related User Story:** [02: Conditional Writable /work Mount](../user-stories/02-conditional-writable-work-mount.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (`dockerRunArgs` branch in `internal/sandbox/docker.go`) | Mirrors the existing `--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` pattern (`docker.go:129`) |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Table-driven cases added to `sandbox_test.go` or `docker_test.go` |
| Key Dependencies | none (stdlib only) | `cfg.WorkSize` (Story 1's `DockerConfig` field) is consumed, not introduced, here |

## Related Files
- `internal/sandbox/docker.go` - modify: `dockerRunArgs` (line 110), `Writable:true` branch replaces `-v spec.SnapshotDir:/work:ro` with `-v spec.SnapshotDir:/src:ro` plus a new `--tmpfs /work:rw,exec,size=" + cfg.WorkSize` entry, flag-ordering and `rw,exec,size=` suffix mirrored exactly from the `/scratch` tmpfs at line 129
- `internal/sandbox/sandbox_test.go` - modify: new table-driven case(s) (alongside or extending `TestDockerRunArgs_HardeningFlagsPresent`) asserting the `Writable:true` argv contains `/src:ro` and `--tmpfs /work:rw,exec,size=` and does NOT contain the old `/work:ro` bind form
- `internal/sandbox/docker_test.go` - reference/optional: existing table-driven test patterns (e.g. `fakeDockerExitBody`, `TestDockerBackendRun_RuntimeExitCodesAreBackendErrors`) may host the new cases instead of `sandbox_test.go` if that better fits file organization

## Happy Path Scenarios
**Scenario 1: Writable:true mounts the snapshot read-only at /src**
- **Given** `RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the returned argv, joined with spaces, contains the literal substring `/tmp/snap:/src:ro` and does not contain `/tmp/snap:/work:ro`

**Scenario 2: Writable:true adds a real writable /work tmpfs**
- **Given** the same `RunSpec` as Scenario 1, with `cfg.WorkSize` set to `"128m"`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the argv contains `--tmpfs /work:rw,exec,size=128m`, giving `/work` real writable backing under the container's existing `--read-only` rootfs flag (`docker.go:117`), which is itself still present and unweakened

## Edge Cases
**Edge Case 1: /scratch tmpfs remains untouched alongside the new /work tmpfs**
- **Given** `Writable: true`
- **When** `dockerRunArgs` builds the argv
- **Then** the pre-existing `--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` entry (line 129) still appears unchanged — the new `/work` tmpfs is additive, not a replacement of `/scratch`

**Edge Case 2: --workdir /work is consistent with the new mount target**
- **Given** `Writable: true`
- **When** `dockerRunArgs` builds the argv
- **Then** `--workdir /work` (line 139) still points at the writable tmpfs (not `/src`), so the caller's command executes with its working directory on the writable path

**Edge Case 3: cfg.WorkSize empty produces an empty size, not a crash**
- **Given** `Writable: true` and `cfg.WorkSize == ""` (a caller that bypasses `DefaultDockerConfig()`)
- **When** `dockerRunArgs` builds the argv
- **Then** the function does not panic or error; it emits `--tmpfs /work:rw,exec,size=` with an empty size value, matching the "no new validation layer" decision from Story 1 (Docker itself will reject the malformed flag at `docker run` time, not at argv-build time)

## Error Conditions
**Error Scenario 1: spec.validate() failures pre-empt the mount branch**
- **Given** an invalid `RunSpec` (e.g. `SnapshotDir` containing `:`, or a relative path) with `Writable: true`
- **When** `dockerRunArgs` is called
- **Then** `spec.validate()` (line 111, unchanged by this story) returns its existing error before the `Writable` branch is ever reached — `Writable` does not change validation behavior
- Error message: unchanged, e.g. `"sandbox: RunSpec.SnapshotDir must not contain ':' (mount-spec injection), got %q"`
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

**Error Scenario 2: Malformed cfg.WorkSize surfaces as a Docker CLI error, not a Go error**
- Error message: a Docker daemon error such as `invalid size: 'abc'` surfaces from `docker run` itself (captured in `RunResult.Output`/backend fault handling in `Run`, `docker.go:250-273`) — `dockerRunArgs` itself does not validate or error on a malformed `WorkSize`, consistent with Story 1's documented decision.
- HTTP status / error code: not applicable — surfaces as a non-zero `docker run` exit classified by `Run`'s existing exit-code handling (`ec >= 125` => backend fault).

## Performance Requirements
- **Response Time:** No measurable change — `dockerRunArgs` remains pure and allocation-light; one additional conditional append to the args slice per call.
- **Throughput:** No change — the `Writable:true` branch runs the same number of statements regardless of call volume; no loops or I/O added.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** `spec.SnapshotDir`'s existing `:`-injection guard (`sandbox.go:61-63`) applies identically to the `/src:ro` mount target as it did to `/work:ro` — the guard is validated before the branch, not per-target, so no new injection surface is introduced by renaming the mount target string.
- **Mount-spec injection guard preserved:** The new `--tmpfs /work:rw,exec,size=<cfg.WorkSize>` entry is built from `cfg.WorkSize` (an operator-controlled config value, mirroring the trust level of `cfg.ScratchSize`), never from `spec` (caller/request-controlled) — this AC does not introduce any new caller-controlled string into a mount-spec position.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `RunSpec{Command: [...], SnapshotDir: "/tmp/snap", Writable: true}` with `cfg.WorkSize` set via `DefaultDockerConfig()` (Story 1's default) and an explicit override case (e.g. `"128m"`); one additional case with `cfg.WorkSize == ""` for Edge Case 3.
**Mock/Stub Requirements:** None — `dockerRunArgs` is pure (no `docker` shim, no filesystem, no daemon needed).

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `Writable:true` argv contains `/src:ro` and does not contain `/work:ro` (the old bind form)
- [ ] `Writable:true` argv contains `--tmpfs /work:rw,exec,size=<cfg.WorkSize>`, mirroring the `/scratch` tmpfs flag pattern exactly
- [ ] `--tmpfs /scratch:...` and `--workdir /work` remain present and unchanged when `Writable` is `true`
- [ ] `spec.validate()` behavior (including the `:`-injection guard) is unaffected by the `Writable` branch

**Manual Review:**
- [ ] Code reviewed and approved

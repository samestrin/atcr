# Acceptance Criteria: Writable:true Mounts /src Read-Only and /work as a tmpfs

**Related User Story:** [02: Conditional Writable /work Mount](../user-stories/02-conditional-writable-work-mount.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (`dockerRunArgs` branch in `internal/sandbox/docker.go`) | Mirrors the existing `--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` pattern (`docker.go:129`) |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Table-driven cases added to `sandbox_test.go` or `docker_test.go` |
| Key Dependencies | none (stdlib only) | `cfg.WorkSize` (Story 1's `DockerConfig` field) is consumed, not introduced, here |
| Mount Semantics Reference | `../documentation/docker-tmpfs-and-read-only-mounts.md` | tmpfs `rw,exec,size=` options; ephemeral (dies with the container); mounting over existing data; under `--read-only` a bare bind-target rename still `EROFS` — only an explicit new `--tmpfs` gives `/work` writable backing |

## Related Files
- `internal/sandbox/docker.go` - modify: `dockerRunArgs` (line 110), `Writable:true` branch replaces `-v spec.SnapshotDir:/work:ro` with `-v spec.SnapshotDir:/src:ro` plus a new `--tmpfs /work:rw,exec,size=" + cfg.WorkSize` entry, flag-ordering and `rw,exec,size=` suffix mirrored exactly from the `/scratch` tmpfs at line 129
- `internal/sandbox/sandbox_test.go` - modify: new table-driven case(s) (alongside or extending `TestDockerRunArgs_HardeningFlagsPresent`) asserting the `Writable:true` argv contains `/src:ro` and `--tmpfs /work:rw,exec,size=` and does NOT contain the old `/work:ro` bind form
- `internal/sandbox/docker_test.go` - reference/optional: existing table-driven test patterns (e.g. `fakeDockerExitBody`, `TestDockerBackendRun_RuntimeExitCodesAreBackendErrors`) may host the new cases instead of `sandbox_test.go` if that better fits file organization

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker.go:110` (`dockerRunArgs`) — modify (`Writable:true` branch replaces the `:140` bind with `-v <SnapshotDir>:/src:ro` and adds `--tmpfs /work:rw,exec,size=<cfg.WorkSize>`)
- `internal/sandbox/docker.go:129` (existing `--tmpfs /scratch:rw,exec,size=` pattern to mirror exactly) — reference-only
- `internal/sandbox/sandbox_test.go:35` — extend (new sibling cases only; the `TestDockerRunArgs_HardeningFlagsPresent` anchor at `:55` stays unmodified)
- `internal/sandbox/docker_test.go` — extend (alternative home for the new table-driven cases)

## Happy Path Scenarios
**Scenario 1: Writable:true mounts the snapshot read-only at /src**
- **Given** `RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the returned argv, joined with spaces, contains the literal substring `/tmp/snap:/src:ro` and does not contain `/tmp/snap:/work:ro`

**Scenario 2: Writable:true adds a real writable /work tmpfs**
- **Given** the same `RunSpec` as Scenario 1, with `cfg.WorkSize` set to `"128m"`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the argv contains `--tmpfs /work:rw,exec,size=128m`, giving `/work` real writable backing under the container's existing `--read-only` rootfs flag (`docker.go:117`), which is itself still present and unweakened
- **And** the mount appears as two adjacent argv elements — `"--tmpfs"` immediately followed by `"/work:rw,exec,size=128m"` — mirroring the `/scratch` construction at `docker.go:129` exactly; `exec` (not `noexec`) is required so build tools can execute binaries and scripts from the working tree, and the tmpfs is memory-backed and removed when the `--rm` container stops, so writes under `/work` never reach the host (`../documentation/docker-tmpfs-and-read-only-mounts.md`)

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

**Edge Case 4: The /work tmpfs obscures any image content at /work**
- **Given** `Writable: true` and a validation image that ships files at `/work`
- **When** the container starts with `--tmpfs /work:rw,exec,size=<cfg.WorkSize>`
- **Then** Docker's mount-over-existing-data semantics hide whatever the image had at `/work` — the only content `/work` ever holds is the source tree the setup step copies in (AC [02-03](02-03-writable-setup-step-copies-src-into-work.md)); nothing image-provided at that path is reachable (semantics per `../documentation/docker-tmpfs-and-read-only-mounts.md`)

**Edge Case 5: Mount entries keep their element form and the /src bind takes the vacated /work:ro position**
- **Given** `Writable: true`
- **When** `dockerRunArgs` builds the argv
- **Then** the snapshot bind appears as two distinct argv elements — `"-v"` immediately followed by `"<SnapshotDir>:/src:ro"` — occupying the same argv position the `/work:ro` bind occupies in the `Writable:false` path (the trailing mount entry at `docker.go:140`, immediately before the payload branch at `:142`), and the new `"--tmpfs"` / `"/work:rw,exec,size=<cfg.WorkSize>"` pair is added within the same mount block; every pre-existing flag's relative order is untouched

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
- **Ephemerality (no host mutation):** the `/work` tmpfs is memory-backed and destroyed with the `--rm` container, so the writable surface — and anything written into it — dies with the run; the host tree under `SnapshotDir` stays reachable only through the `:ro` bind at `/src`. This is the epic-level "ephemeral copy" guarantee (original-requirements.md Acceptance Criterion 4), with semantics sourced from `../documentation/docker-tmpfs-and-read-only-mounts.md`.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** `RunSpec{Command: [...], SnapshotDir: "/tmp/snap", Writable: true}` with `cfg.WorkSize` set via `DefaultDockerConfig()` (Story 1's default) and an explicit override case (e.g. `"128m"`); one additional case with `cfg.WorkSize == ""` for Edge Case 3.
**Mock/Stub Requirements:** None — `dockerRunArgs` is pure (no `docker` shim, no filesystem, no daemon needed).
**Naming:** per the package convention (`TestDockerRunArgs_<Scenario>`), e.g. `TestDockerRunArgs_WritableTrueMountsSrcROAndWorkTmpfs`.
**Assertion style:** daemon-free argv-level assertions per the package's existing pattern — joined-substring checks (`assert.Contains`/`assert.NotContains` on `strings.Join(args, " ")`) plus element-form checks that locate the `"--tmpfs"` element and assert the NEXT element equals `"/work:rw,exec,size=<cfg.WorkSize>"` (and likewise `"-v"` → `"<SnapshotDir>:/src:ro"`), so a malformed single-token mount cannot pass.

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
- [ ] The `/work` tmpfs is asserted as two adjacent argv elements (`"--tmpfs"`, `"/work:rw,exec,size=<cfg.WorkSize>"`) and the `/src:ro` bind as two (`"-v"`, `"<SnapshotDir>:/src:ro"`), with all pre-existing flags' relative order unchanged

**Manual Review:**
- [ ] Code reviewed and approved

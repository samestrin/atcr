# Acceptance Criteria: Writable:true Argv/Stdin Shape Tests for Both RunSpec Modes

**Related User Story:** [05: Regression Proof and Documentation Parity](../user-stories/05-regression-proof-and-docs-parity.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go test cases in `internal/sandbox/sandbox_test.go` | Table-driven, extending the existing pattern used by `TestDockerRunArgs_HardeningFlagsPresent` and `TestDockerRunArgs_ScriptUsesStdinShell` |
| Test Framework | `go test` + `testify` (`assert`/`require`) | No new test framework dependency |
| Key Dependencies | none (stdlib only) | Exercises `dockerRunArgs` built by Stories 1-3; no new helper needed |

## Related Files
- `internal/sandbox/sandbox_test.go` - modify: add new `Writable:true` cases (Command mode and Script mode) as additive table rows or sibling test functions, following the pattern of `TestDockerRunArgs_HardeningFlagsPresent` (line 35) for Command mode and `TestDockerRunArgs_ScriptUsesStdinShell` (line 83) for Script mode — the source of the argv/stdin shapes under test
- `internal/sandbox/docker_test.go` - modify: alternative/additional location for `DockerBackend`-level `Writable:true` argv assertions if the case is expressed at the backend level rather than the pure `dockerRunArgs` level
- `internal/sandbox/docker.go` - reference only: `dockerRunArgs` (line 110), the function under test; Story 2/3 add the `spec.Writable` branch here (the `/src:ro` mount, the `--tmpfs /work:rw,exec,size=` flag, and the `cp -a`/shell-wrap setup injection) that this AC's tests assert against, without this AC modifying that function itself

## Happy Path Scenarios
**Scenario 1: Command-mode Writable:true argv shape**
- **Given** `RunSpec{Command: []string{"npm", "run", "build"}, SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the joined argv contains `/tmp/snap:/src:ro` (the read-only source mount), a `--tmpfs /work:rw,exec,size=` flag (the writable overlay), and a shell-wrap invocation (`/bin/sh -c` with `cp -a` and `exec "$@"`) that carries the original `npm run build` argv positionally rather than interpolated into the wrap string

**Scenario 2: Script-mode Writable:true stdin shape**
- **Given** `RunSpec{Script: "npm run build\n", SnapshotDir: "/tmp/snap", Writable: true}`
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the joined argv contains `/tmp/snap:/src:ro`, the `--tmpfs /work:rw,exec,size=` flag, and `-i <image> /bin/sh -s` exactly as the existing `Writable:false` Script-mode shape does, while the script body streamed to stdin (verified via the `Run`/stdin-construction path, not argv) is prepended with a `cp -a /src/. /work/ && cd /work` setup line before the original script content

**Scenario 3: Writable:true argv never contains the plain read-only /work mount**
- **Given** either RunSpec from Scenario 1 or Scenario 2 with `Writable: true`
- **When** `dockerRunArgs` runs
- **Then** the joined argv does NOT contain the `Writable:false` literal `SnapshotDir + ":/work:ro"` substring — the two mount shapes (`/src:ro` + tmpfs `/work` vs. bare `/work:ro`) are mutually exclusive per RunSpec

## Edge Cases
**Edge Case 1: WorkSize flows from DockerConfig into the tmpfs size flag**
- **Given** a `DockerConfig` with a non-default `WorkSize` (e.g. `"512m"`) and a `Writable:true` RunSpec
- **When** `dockerRunArgs` builds the argv
- **Then** the `--tmpfs /work:rw,exec,size=512m` flag reflects the configured `WorkSize`, not a hardcoded literal

**Edge Case 2: The original command argv is preserved verbatim after the wrap**
- **Given** a Command-mode `Writable:true` RunSpec with a multi-token command (`["npm", "run", "build", "--", "--production"]`)
- **When** `dockerRunArgs` builds the shell-wrapped argv
- **Then** every original token is still present and in order after the `--` positional-args separator, so the wrap injects no token reordering or loss

## Error Conditions
**Error Scenario 1: N/A for this AC — no new error path**
- These are pure-function argv-shape assertions; `spec.validate()` (`internal/sandbox/sandbox.go`) is unchanged by `Writable` and its existing validation errors (missing `SnapshotDir`, both/neither `Command`/`Script` set, colon-injection in `SnapshotDir`) apply identically regardless of `Writable`.
- Error message: not applicable — no new error is introduced by adding these test cases.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** Not applicable — these are unit tests over a pure function; test execution time is negligible (sub-millisecond per case, no I/O).
- **Throughput:** Not applicable.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go test, no external auth surface.
- **Input Validation:** The Command-mode assertion must explicitly verify the original command tokens are passed via `-- "$@"` positional expansion rather than string-interpolated into the `/bin/sh -c '...'` script text, preserving the no-shell-injection invariant already documented for `Run`'s script-stdin handling.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** New `RunSpec` literals with `Writable: true` for both Command and Script modes, following the existing fixture shapes (`SnapshotDir: "/tmp/snap"`) used by `TestDockerRunArgs_HardeningFlagsPresent` and `TestDockerRunArgs_ScriptUsesStdinShell`.
**Mock/Stub Requirements:** None — `dockerRunArgs` is pure (no `docker` shim, no filesystem, no daemon needed) for the argv-shape assertions in this AC; the functional write-proof lives in AC 05-02.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] New Command-mode `Writable:true` test asserts `/src:ro`, `--tmpfs /work:rw,exec,size=`, and the shell-wrap with positional `-- "$@"` argument passing
- [ ] New Script-mode `Writable:true` test asserts `/src:ro`, `--tmpfs /work:rw,exec,size=`, `-i <image> /bin/sh -s`, and a prepended `cp -a` setup line in the stdin body
- [ ] New cases are added as additive table rows or sibling test functions — no existing assertion in `TestDockerRunArgs_HardeningFlagsPresent` or `TestDockerRunArgs_ScriptUsesStdinShell` is edited
- [ ] `go test -run TestDockerRunArgs_HardeningFlagsPresent` and `go test -run TestDockerRunArgs_ScriptUsesStdinShell` show zero behavior diff before/after this AC's additions

**Manual Review:**
- [ ] Code reviewed and approved

# Acceptance Criteria: Writable:false Argv Stays Byte-Identical

**Related User Story:** [02: Conditional Writable /work Mount](../user-stories/02-conditional-writable-work-mount.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go function (`dockerRunArgs` branch in `internal/sandbox/docker.go`) | Pure function, no I/O — argv is asserted directly |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Matches existing `internal/sandbox` test style |
| Key Dependencies | none (stdlib only) | `strconv`, `strings` already imported in `docker.go` |
| Regression Anchors | `TestDockerRunArgs_HardeningFlagsPresent` (`internal/sandbox/sandbox_test.go:35`, `/work:ro` assertion at `:55`) and `TestDockerRunArgs_ScriptUsesStdinShell` (`sandbox_test.go:83`) | Both must pass with zero edits; they pin the `Writable:false` Command-mode and Script-mode argv shapes respectively |

## Related Files
- `internal/sandbox/docker.go` - modify: `dockerRunArgs` (line 110) gains an `if spec.Writable` branch around the trailing mount-list construction (line 140); the `false`/default branch must keep the exact existing statement `"-v", spec.SnapshotDir + ":/work:ro"` textually unchanged
- `internal/sandbox/sandbox_test.go` - reference only (not modified): `TestDockerRunArgs_HardeningFlagsPresent` (line 35), whose assertion at line 55 (`assert.Contains(t, joined, "/tmp/snap:/work:ro", ...)`) is the regression anchor and must pass with zero edits to the test file
- `internal/sandbox/docker.go` - reference only: `Preflight` (line 281-316) builds its trivial-run argv via `dockerRunArgs(b.cfg, RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir})` (line 308), leaving `Writable` at its zero value (`false`) — this call site is the control-group proof and must not be touched

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker.go:110` (`dockerRunArgs`) — modify
- `internal/sandbox/docker.go:281-316` (`Preflight` control group; trivial-run argv built at `:308`) — reference-only
- `internal/sandbox/sandbox_test.go:35,55` (`TestDockerRunArgs_HardeningFlagsPresent` regression anchor) — reference-only (must stay green unmodified; any new cases are added as sibling tests, never by editing the anchor)
- `internal/sandbox/sandbox_test.go:83` (`TestDockerRunArgs_ScriptUsesStdinShell`) — reference-only (second anchor, pins the `Writable:false` Script-mode argv shape `-i <image> /bin/sh -s`)
- `internal/sandbox/docker_test.go` — extend (optional home for new sibling cases; the existing `TestDockerBackend_Preflight_*` tests at `:55-118` are the executable Preflight control group)
- `internal/tools/exec_tools.go:178,215` — reference-only (the two `--exec` call sites construct `RunSpec` without `Writable`; zero-value control group)
- `internal/verify/autofix_exec_test.go:23,43,56` (`fakeDockerRecording`, `runArgsLine`, `TestResolveAutoFixSandbox_BuildsAndPreflights`) — reference-only (records the exact `docker run` argv Preflight emits with `Writable` at its zero value)

## Happy Path Scenarios
**Scenario 1: Default RunSpec produces today's exact mount**
- **Given** `RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}` with `Writable` left unset (zero value `false`)
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the returned argv, joined with spaces, contains the literal substring `/tmp/snap:/work:ro` and does not contain `/src:ro` or a second `--tmpfs /work` entry

**Scenario 2: Preflight's control-group call is unaffected**
- **Given** `Preflight` builds `RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir}` (no `Writable` set)
- **When** `dockerRunArgs` runs internally as part of `Preflight`
- **Then** the trivial-run argv is byte-identical to the pre-story argv — same elements, same order, same count — (`-v <tmpDir>:/work:ro`, `--workdir /work`, no `/src` mount, no extra `--tmpfs /work`); the executable pins for this control group are `TestResolveAutoFixSandbox_BuildsAndPreflights` (`internal/verify/autofix_exec_test.go:56`, asserting the recorded argv via `fakeDockerRecording` at `:23` / `runArgsLine` at `:43`) and the `TestDockerBackend_Preflight_*` tests (`internal/sandbox/docker_test.go:55-118`), all of which must stay green unmodified

**Scenario 3: Full Writable:false argv equals the pre-story golden slice**
- **Given** `RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}` with `Writable` at its zero value, and a `DefaultDockerConfig()` config
- **When** `dockerRunArgs(cfg, spec)` is called inside a NEW sibling test (the anchor test itself is not edited)
- **Then** the returned `[]string` equals the pre-story argv element-for-element — same length, same order, same values — asserted with `assert.Equal` against a golden slice literal; "byte-identical" in this AC means exact slice equality, not merely substring containment

## Edge Cases
**Edge Case 1: Every existing `--exec` call site is unaffected**
- **Given** `internal/tools/exec_tools.go` lines 178 and 215 construct `RunSpec{...}` literals that do not set `Writable`
- **When** those call sites run through `dockerRunArgs` unchanged by this story
- **Then** their behavior and resulting argv are identical to pre-story behavior, since `Writable` defaults to `false` and this AC's branch preserves that path verbatim

**Edge Case 2: Hardening flags are untouched by the branch**
- **Given** the same `RunSpec` used in `TestDockerRunArgs_HardeningFlagsPresent`
- **When** `dockerRunArgs` runs with the new `Writable`-conditional branch present in the source
- **Then** `--network none`, `--read-only`, `--cap-drop ALL`, `--security-opt no-new-privileges`, `--user <cfg.User>`, `--memory`, `--cpus`, `--pids-limit`, and `--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` all still appear, unreordered and unweakened — and the cache-redirection env block (`-e HOME=/scratch`, `-e TMPDIR=/scratch`, `-e XDG_CACHE_HOME=/scratch/.cache`, `-e GOCACHE=/scratch/.gocache`, `-e GOTMPDIR=/scratch`, `docker.go:134-138`) and `--workdir /work` (`docker.go:139`) are likewise present and unchanged

**Edge Case 3: Writable:false Script-mode argv stays byte-identical**
- **Given** `RunSpec{Script: "echo hi\nexit 3\n", SnapshotDir: "/tmp/snap"}` with `Writable` unset (the fixture from `TestDockerRunArgs_ScriptUsesStdinShell`, `sandbox_test.go:83`)
- **When** `dockerRunArgs(cfg, spec)` is called
- **Then** the argv tail is exactly `-i <cfg.Image> /bin/sh -s` and the script body never appears in argv — identical to pre-story behavior; `TestDockerRunArgs_ScriptUsesStdinShell` stays green unmodified as this AC's second anchor

## Error Conditions
**Error Scenario 1: N/A — no new error path in the false branch**
- This AC only asserts existing behavior is preserved; `spec.validate()` (`internal/sandbox/sandbox.go:43`) is untouched and its existing errors (e.g. `"sandbox: RunSpec.SnapshotDir is required"`) are unaffected by the `Writable` branch.
- Error message: no new error message is introduced by this AC.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

**Error Scenario 2: A regression in the false-branch mount is a test failure, not a runtime error**
- If a future edit to the `false` branch accidentally changes the mount target or drops `:ro`, `TestDockerRunArgs_HardeningFlagsPresent`'s assertion at `sandbox_test.go:55` fails at `go test` time — the AC's Definition of Done treats that test failure as the detection mechanism, since there is no runtime error to observe (the container would still start, just without the intended read-only guarantee).

## Performance Requirements
- **Response Time:** No measurable change — `dockerRunArgs` remains a pure, allocation-light function; adding a boolean branch does not change its asymptotic cost.
- **Throughput:** No change — the `false` branch executes the same statement count as before the story.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** The `false` branch must not introduce any new interpolation of `spec.SnapshotDir` or `cfg` fields into the mount spec beyond what already exists; `spec.validate()`'s `:`-injection guard (`sandbox.go:61-63`) continues to apply identically regardless of which branch is taken, since validation happens before the branch (line 111).
- **Documented guarantee preserved (PRESERVE anchors):** `docs/execution.md:51-62` and `:86-90` plus the `internal/sandbox` package doc (`sandbox.go:1-15`) pin a hard read-only-`/work` guarantee for `--exec`; per `../documentation/current-sandbox-guarantees.md` these are PRESERVE anchors, and this AC's byte-identical `Writable:false` path is what keeps them textually true — any drift in the false branch is a bug by definition, even if the container would still start.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Reuse the existing `RunSpec{Command: []string{"go", "test", "./..."}, SnapshotDir: "/tmp/snap"}` fixture from `TestDockerRunArgs_HardeningFlagsPresent`; no new fixtures required for this AC.
**Mock/Stub Requirements:** None — `dockerRunArgs` is pure (no `docker` shim, no filesystem, no daemon needed).
**Naming:** per the package convention (`TestDockerRunArgs_<Scenario>`), add new SIBLING tests only (e.g. `TestDockerRunArgs_WritableFalseGoldenArgv` for Scenario 3); `TestDockerRunArgs_HardeningFlagsPresent` and `TestDockerRunArgs_ScriptUsesStdinShell` are NOT edited.
**Assertion style:** daemon-free argv-level assertions per the package's existing pattern — `strings.Join(args, " ")` for substring containment checks, and direct slice comparison (`assert.Equal` against a golden `[]string`) for the byte-identical guarantee in Scenario 3.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `TestDockerRunArgs_HardeningFlagsPresent` passes with zero edits to its assertions, including the `/tmp/snap:/work:ro` check at `sandbox_test.go:55`
- [ ] The `Writable:false` branch of `dockerRunArgs` is textually unchanged from pre-story code (`-v spec.SnapshotDir + ":/work:ro"`)
- [ ] `Preflight`'s trivial-run call (`docker.go:308`) produces an argv with no `/src` mount and no second `--tmpfs /work` entry
- [ ] No existing hardening flag is reordered, weakened, or made conditional by the new branch
- [ ] A new sibling test asserts the full `Writable:false` argv equals the pre-story golden slice element-for-element (Command mode), and `TestDockerRunArgs_ScriptUsesStdinShell` (`sandbox_test.go:83`) also stays green unmodified (Script mode)

**Manual Review:**
- [ ] Code reviewed and approved

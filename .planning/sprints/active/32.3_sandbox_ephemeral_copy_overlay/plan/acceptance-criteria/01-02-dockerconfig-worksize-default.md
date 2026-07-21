# Acceptance Criteria: DockerConfig Gains a WorkSize Tunable with a Sane Default

**Related User Story:** [01: Opt-In Writable Configuration Surface](../user-stories/01-opt-in-writable-configuration-surface.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct field (`DockerConfig.WorkSize string`) + default assignment in `DefaultDockerConfig()` | Added to `internal/sandbox/docker.go` |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Matches existing `docker_test.go` style |
| Key Dependencies | none (stdlib only) | Follows the exact `ScratchSize` field-plus-default pattern already in the file |

## Related Files
- `internal/sandbox/docker.go` - modify: add `WorkSize string` field to `DockerConfig` directly beside `ScratchSize` (line ~40), with a matching doc comment; set its default in `DefaultDockerConfig()` next to `ScratchSize: "64m"` (line ~61), sized larger than 64m to fit a full source-tree copy
- `internal/sandbox/docker_test.go` - modify: add a test asserting `DefaultDockerConfig().WorkSize` is non-empty and strictly larger (in equivalent bytes) than `DefaultDockerConfig().ScratchSize`

### Related Files (from codebase-discovery.json)
- `internal/sandbox/docker.go:24-50` (`DockerConfig` struct; `ScratchSize` field at :40) — modify: add `WorkSize string` directly beside `ScratchSize` with a matching doc comment
- `internal/sandbox/docker.go:53` (`DefaultDockerConfig()`; `ScratchSize: "64m"` default at :61) — modify: set the `WorkSize` default in the same struct literal, strictly larger than `"64m"`
- `internal/sandbox/docker.go:110` (`dockerRunArgs`) — reference-only in this story: must not read `WorkSize` yet (mount wiring is a later story)
- `internal/sandbox/docker_test.go` — extend (additive only): new default-value test; no existing test body modified
- `internal/registry/sandbox.go:20-38` (`SandboxConfig`) — reference-only, explicitly NOT modified: no YAML knob for `WorkSize` (deliberate code-only default per plan.md Refinement Decisions 2026-07-21, mirroring `ScratchSize`'s current treatment)

## Happy Path Scenarios
**Scenario 1: DefaultDockerConfig sets a sane WorkSize**
- **Given** `cfg := DefaultDockerConfig()`
- **When** `cfg.WorkSize` is read
- **Then** it is a non-empty docker `--tmpfs`/size-style string (e.g. `"512m"`) sized deliberately larger than `cfg.ScratchSize` ("64m"), documented in the field's doc comment as sized for a full source-tree copy rather than just a build cache
- **And** the doc comment names the later consumer (`--tmpfs /work:rw,exec,size=<WorkSize>`, mirroring the existing `--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` pattern in `dockerRunArgs`) and repeats the implicit image requirement flagged in plan.md's Refinement Decisions (2026-07-21): the writable-overlay path requires the run image to ship `/bin/sh` and a `cp` supporting `-a` (true for `alpine`/`golang`-family images, false for distroless/scratch)

**Scenario 2: Struct-literal DockerConfig without WorkSize keeps zero value**
- **Given** a `DockerConfig{}` struct literal built directly (bypassing `DefaultDockerConfig()`), as some tests in `docker_test.go` do for `DockerPath`/`CPUs` overrides
- **When** `WorkSize` is not set
- **Then** it is the empty string (Go zero value) — this story adds no fallback/defaulting logic for a raw struct literal, mirroring `ScratchSize`'s current (undefaulted-outside-`DefaultDockerConfig`) treatment

## Edge Cases
**Edge Case 1: Field is declared but not yet consumed**
- **Given** `WorkSize` is set (to the default or any other value) on a `DockerConfig` passed into `dockerRunArgs`
- **When** `dockerRunArgs` builds the `docker run` argv
- **Then** the returned argv is unchanged from before this story — `WorkSize` is not read anywhere in `dockerRunArgs`, `Run`, or `Preflight` in this story's diff (mount-argument wiring is a later story)

**Edge Case 2: Value follows docker size-string convention, not validated here**
- **Given** `WorkSize` is a raw string, like `ScratchSize`
- **When** an operator or test sets an arbitrary string (e.g. `"not-a-size"`)
- **Then** no validation error is raised by this story — `ScratchSize` similarly has no validation today, and adding validation for `WorkSize` is explicitly out of scope per the story's Constraints (no new validation logic required)

**Edge Case 3: `NewDockerBackend` defaulting logic is unchanged**
- **Given** `NewDockerBackend` (`internal/sandbox/docker.go:80-95`) floors `DockerPath`/`MaxOutputBytes`/`Timeout`/`MaxConcurrent` when built from a partial config, while `ScratchSize` receives no such floor
- **When** this story lands
- **Then** `NewDockerBackend` gains no `WorkSize` floor either — strictly mirroring `ScratchSize`'s treatment — and the four existing floor branches are unmodified

## Error Conditions
**Error Scenario 1: N/A — no new error paths**
- This story adds a passive config field with a default value; it introduces no new validation and therefore no new Go error values.
- Error message: no new error message is introduced by this AC.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** No measurable change — a config default assignment adds negligible (sub-microsecond) cost to `DefaultDockerConfig()`.
- **Throughput:** No change — `WorkSize` is not read by any run-time code path in this story.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** No `internal/registry` YAML knob is created for `WorkSize` in this story (a deliberate scope exclusion per the plan's Refinement Decisions, mirroring `ScratchSize`'s current code-only treatment) — so there is no new operator-facing config-injection surface to validate. If a later story exposes `WorkSize` via YAML, that story owns adding range/format validation; this AC only asserts the Go-level default is sane.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** No fixtures beyond calling `DefaultDockerConfig()`; reuse existing `docker_test.go` patterns (see tests around `ScratchSize`/`cfg.MaxConcurrent`).
**Mock/Stub Requirements:** None — no `docker` shim or daemon interaction needed for this field-default assertion.
**Suggested Test Name / Assertions:** `TestDefaultDockerConfig_WorkSizeDefault` (new, in `internal/sandbox/docker_test.go`): `assert.NotEmpty(t, cfg.WorkSize)`, then parse both `cfg.WorkSize` and `cfg.ScratchSize` with the same size-suffix helper (docker `k`/`m`/`g` convention, e.g. a small test-local parser) and assert equivalent-bytes(`WorkSize`) is strictly greater than equivalent-bytes(`ScratchSize`). Additionally verify the code-only-default decision mechanically, outside the test: `grep -rn "WorkSize\|work_size" internal/registry/` returns zero matches (no YAML knob, per plan.md Refinement Decisions 2026-07-21).

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/sandbox/...`)
- [x] No linting errors (`go vet ./...` / project lint gate)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `DockerConfig` struct in `internal/sandbox/docker.go` has a `WorkSize string` field beside `ScratchSize` (docker.go:40), with a doc comment matching the existing field's style
- [x] The doc comment states the default is sized for a full source-tree copy (larger than `ScratchSize`'s `"64m"`), names the later consumer (`--tmpfs /work:rw,exec,size=<WorkSize>`), and flags the implicit `/bin/sh` + `cp -a` image requirement (not distroless/scratch) per plan.md Refinement Decisions (2026-07-21)
- [x] `DefaultDockerConfig()` (docker.go:53-66) sets `WorkSize` to a value strictly larger than the `ScratchSize` default (`"64m"`) in equivalent bytes, verified by a unit test
- [x] `dockerRunArgs`, `Run`, and `Preflight` do not read or branch on `WorkSize` in this story's diff, and `NewDockerBackend` gains no `WorkSize` floor
- [x] No `internal/registry` YAML field is added for `WorkSize` (verified: zero `grep` matches for `WorkSize`/`work_size` under `internal/registry/`)

**Manual Review:**
- [ ] Code reviewed and approved

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

## Happy Path Scenarios
**Scenario 1: DefaultDockerConfig sets a sane WorkSize**
- **Given** `cfg := DefaultDockerConfig()`
- **When** `cfg.WorkSize` is read
- **Then** it is a non-empty docker `--tmpfs`/size-style string (e.g. `"512m"`) sized deliberately larger than `cfg.ScratchSize` ("64m"), documented in the field's doc comment as sized for a full source-tree copy rather than just a build cache

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

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `DockerConfig` struct in `internal/sandbox/docker.go` has a `WorkSize string` field beside `ScratchSize`, with a doc comment matching the existing field's style
- [ ] `DefaultDockerConfig()` sets `WorkSize` to a value larger than the `ScratchSize` default (`"64m"`), verified by a unit test
- [ ] `dockerRunArgs`, `Run`, and `Preflight` do not read or branch on `WorkSize` in this story's diff
- [ ] No `internal/registry` YAML field is added for `WorkSize`

**Manual Review:**
- [ ] Code reviewed and approved

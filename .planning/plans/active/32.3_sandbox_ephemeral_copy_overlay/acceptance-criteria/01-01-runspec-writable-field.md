# Acceptance Criteria: RunSpec Gains an Opt-In Writable Field

**Related User Story:** [01: Opt-In Writable Configuration Surface](../user-stories/01-opt-in-writable-configuration-surface.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Component Type | Go struct field (`RunSpec.Writable bool`) | Added to `internal/sandbox/sandbox.go` |
| Test Framework | `go test` + `testify` (`assert`/`require`) | Matches existing `internal/sandbox` test style |
| Key Dependencies | none (stdlib only) | Zero-value `bool` default requires no constructor or init logic |

## Related Files
- `internal/sandbox/sandbox.go` - modify: add `Writable bool` field to the `RunSpec` struct (near `SnapshotDir`, line ~40) with a doc comment stating it defaults to `false` and that `true` layers a writable `/work` overlay (mechanism implemented in a later story)
- `internal/sandbox/sandbox_test.go` - modify: add a test asserting `RunSpec{}` zero value has `Writable == false`
- `internal/tools/exec_tools.go` - reference only (not modified): lines 178 and 215 construct `RunSpec{...}` literals that do not set `Writable`, and must keep compiling and behaving identically

## Happy Path Scenarios
**Scenario 1: Zero-value RunSpec defaults to read-only**
- **Given** a `RunSpec` literal that does not set `Writable` (e.g. `RunSpec{Command: []string{"true"}, SnapshotDir: "/tmp/x"}`)
- **When** the struct is constructed
- **Then** `spec.Writable` is `false` (Go's zero value for `bool`), matching today's implicit read-only behavior

**Scenario 2: Explicit opt-in is representable**
- **Given** a caller that sets `RunSpec{..., Writable: true}`
- **When** the struct is constructed
- **Then** `spec.Writable` is `true` and is readable by any future consumer, with no other field or method affected by this story

## Edge Cases
**Edge Case 1: Field is declared but not yet consumed**
- **Given** `Writable` is set to `true` on a `RunSpec` passed to `dockerRunArgs` or `Backend.Run` in this story's code state
- **When** the run executes
- **Then** behavior is byte-for-byte identical to `Writable: false` — no mount-argument branching exists yet (that wiring is out of scope for this story and lands in a later story in this plan)

**Edge Case 2: Doc comment does not imply the mechanism is implemented**
- **Given** a future maintainer reads the `Writable` field doc comment
- **When** they check its described behavior against the code
- **Then** the comment explicitly states the writable-overlay mechanism is implemented in a later story, avoiding a misleading "this already works" impression

## Error Conditions
**Error Scenario 1: N/A — no new error paths**
- This story adds a passive data field with no validation branch; `RunSpec.validate()` (`internal/sandbox/sandbox.go:43`) is untouched and must continue to return its existing errors (`"sandbox: RunSpec must set exactly one of Command or Script..."`, `"sandbox: RunSpec.SnapshotDir is required"`, etc.) unchanged for every existing input.
- Error message: no new error message is introduced by this AC.
- HTTP status / error code: not applicable (internal Go package, no HTTP surface).

## Performance Requirements
- **Response Time:** No measurable change — this is a struct-field addition with no new computation on any code path.
- **Throughput:** No change — `Writable` is not read by `dockerRunArgs`, `Run`, or `validate()` in this story, so no per-run cost is added.

## Security Considerations
- **Authentication/Authorization:** Not applicable — internal Go package, no external auth surface.
- **Input Validation:** `Writable` is a `bool`, not a string or path, so there is no injection surface (unlike `SnapshotDir`, which is validated against `:`-injection in `validate()`). This story must not add any validation branching that reads `Writable`; doing so is explicitly out of scope and would blur this story's data-model-only boundary with the later mount-wiring story.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** No fixtures beyond in-memory `RunSpec{}` literals; reuse existing `sandbox_test.go` patterns (see `TestDockerRunArgs_HardeningFlagsPresent`).
**Mock/Stub Requirements:** None — this is a pure struct/zero-value assertion, no `docker` shim or filesystem needed.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/... ./internal/tools/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `RunSpec` struct in `internal/sandbox/sandbox.go` has a `Writable bool` field with a doc comment describing the `false` default and the opt-in writable-overlay effect
- [ ] `RunSpec{}` zero value has `Writable == false`, verified by a unit test
- [ ] `RunSpec.validate()` is unmodified and its existing test coverage still passes unchanged
- [ ] Neither `dockerRunArgs` nor `Backend.Run` reads or branches on `Writable` in this story's diff

**Manual Review:**
- [ ] Code reviewed and approved

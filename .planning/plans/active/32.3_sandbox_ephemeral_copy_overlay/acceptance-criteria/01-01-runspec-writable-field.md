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

### Related Files (from codebase-discovery.json)
- `internal/sandbox/sandbox.go:28` (`RunSpec` struct) — modify: add `Writable bool` near `SnapshotDir` (sandbox.go:39-40) with the doc comment described in the scenarios below
- `internal/sandbox/sandbox.go:43` (`RunSpec.validate()`) — reference-only: untouched by this story; must not read or branch on `Writable`
- `internal/sandbox/sandbox_test.go:35` (home of `TestDockerRunArgs_HardeningFlagsPresent`) — extend (additive only): new zero-value test; no existing test body modified
- `internal/tools/exec_tools.go:178,215` — reference-only control group: both `RunSpec{...}` literals leave `Writable` unset and keep the zero value `false` with zero code change

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

**Edge Case 3: Doc comment narrows — never weakens — the package-level read-only guarantee, and flags the image requirement**
- **Given** the package doc (`internal/sandbox/sandbox.go:1-15`) states a hard MUST for every `Backend.Run`: "a read-only view of the snapshot (the run cannot mutate the work tree)" (PRESERVE anchor, see `../documentation/current-sandbox-guarantees.md`)
- **When** the `Writable` field doc comment describes the opt-in
- **Then** it makes the narrowing explicit: under `Writable:true` the snapshot itself stays read-only (mounted at `/src` by the later mount story), only the ephemeral `/work` tmpfs copy is writable, and no host file is ever mutated — the package-level MUST is preserved, not edited. The comment also flags the new implicit image requirement recorded in plan.md's Refinement Decisions (2026-07-21) and `codebase-discovery.json` → `integration_gaps`: `Writable:true` Command mode will wrap execution in `/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"'`, so the run image must ship `/bin/sh` and a `cp` supporting `-a` (true for `alpine`/`golang`-family images via busybox/coreutils, false for distroless/scratch images)

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
**Suggested Test Name / Assertions:** `TestRunSpec_WritableDefaultsToFalse` (new, in `internal/sandbox/sandbox_test.go`, same package so it may call `validate()` directly): assert `RunSpec{}.Writable == false`; assert an explicit `RunSpec{Command: []string{"true"}, SnapshotDir: t.TempDir(), Writable: true}` round-trips `Writable == true`; and assert `validate()` returns `nil` for that spec with `Writable` set to both `true` and `false` (pinning that the field does not interact with the exactly-one-of-Command/Script or `SnapshotDir` checks). Daemon-free — no `writeFakeDocker` (sandbox_test.go:24) needed for this AC.

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/sandbox/... ./internal/tools/...`)
- [ ] No linting errors (`go vet ./...` / project lint gate)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `RunSpec` struct in `internal/sandbox/sandbox.go` has a `Writable bool` field (near `SnapshotDir`, sandbox.go:39-40) with a doc comment describing the `false` default and the opt-in writable-overlay effect
- [ ] The doc comment states the mechanism lands in a later story; narrows (never weakens) the package doc's read-only-snapshot MUST (snapshot stays read-only at `/src`; only the ephemeral `/work` tmpfs copy is writable; no host file is mutated); and flags the implicit `/bin/sh` + `cp -a` image requirement (not distroless/scratch) per plan.md Refinement Decisions (2026-07-21)
- [ ] `RunSpec{}` zero value has `Writable == false`, verified by a unit test
- [ ] `RunSpec.validate()` is unmodified and its existing test coverage still passes unchanged
- [ ] Neither `dockerRunArgs` nor `Backend.Run` reads or branches on `Writable` in this story's diff

**Manual Review:**
- [ ] Code reviewed and approved

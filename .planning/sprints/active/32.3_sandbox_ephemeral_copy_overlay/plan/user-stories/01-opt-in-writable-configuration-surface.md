# User Story 1: Opt-In Writable Configuration Surface

**Plan:** [32.3: Sandbox Writable Overlay for Polyglot Auto-Fix](../plan.md)

## User Story

**As a** Go engineer maintaining the atcr sandbox internals (`internal/sandbox/`)
**I want** an opt-in `RunSpec.Writable` field and a matching `DockerConfig.WorkSize` tunable, both defaulting to today's read-only behavior
**So that** the sandbox's config surface can express "this run needs a writable `/work`" without silently changing behavior for any existing caller (`--exec`'s two call sites, or the read-only guarantee `--exec` depends on)

## Story Context

- **Background:** Epic 32.0 mounts the user's project directory read-only (`SnapshotDir:/work:ro`) inside the Docker sandbox so `--exec` (Epic 11.0, `internal/tools/exec_tools.go`) can safely run model-authored commands without risking host mutation. `RunSpec` (`internal/sandbox/sandbox.go`) and `DockerConfig` (`internal/sandbox/docker.go`) are the shared config types both `--exec` and the upcoming `--auto-fix` sandboxed validation path (`internal/verify/sandboxvalidate.go`) build `RunSpec` values from. This story adds the two new fields only — the mount-arg branching and setup-step injection that consume them are separate, later stories in this plan.
- **Assumptions:** `RunSpec`'s Go zero value for a new bool field is `false`, and Go's implicit zero-initialization means every existing struct literal that does not set the field (both call sites in `internal/tools/exec_tools.go:178,215`) is unaffected without any code change at those call sites. `DockerConfig.WorkSize` can follow the exact same declare-a-field-plus-set-a-default-in-`DefaultDockerConfig`-pattern already used for `ScratchSize` (`internal/sandbox/docker.go:40,61`), with no new validation logic required for this story.
- **Constraints:** `RunSpec.validate()` (`internal/sandbox/sandbox.go:43`) already enforces the exactly-one-of-Command/Script invariant and `SnapshotDir` mount-injection safety checks; this story must not touch or weaken that validation, and `Writable` must not interact with it. No `internal/registry` YAML knob is created for `WorkSize` — per the plan's Refinement Decisions this is a deliberate code-only default, mirroring `ScratchSize`'s current treatment. This story is data-model only: no mount-argument construction, no setup-step shell wrapping, and no `--auto-fix` call-site change belong here (those are later stories).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | None |

## Success Criteria (SMART Format)

- **Specific:** `RunSpec` gains a `Writable bool` field and `DockerConfig` gains a `WorkSize string` field (with a sane default set in `DefaultDockerConfig()`, following the `ScratchSize` pattern exactly), and neither field is read or branched on anywhere yet.
- **Measurable:** `go build ./...` succeeds; the full existing `internal/sandbox` test suite (`docker_test.go`, `sandbox_test.go`) passes unmodified with zero new failures or skips; both `--exec` call sites in `internal/tools/exec_tools.go` compile and pass their existing tests without any edit to those call sites.
- **Achievable:** This is an additive struct-field change plus one default-value line — no control-flow change, so it is low-risk and quick to implement and verify.
- **Relevant:** Every later story in this plan (mount branching, setup-step injection, `--auto-fix` opt-in) depends on `Writable` and `WorkSize` existing as named, documented fields first — this is the foundational data-model story the plan's Story Theme explicitly assigns to it.
- **Time-bound:** Completed as the first story of Sprint 32.3, before any mount-argument or setup-step story begins.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-runspec-writable-field.md) | RunSpec Gains an Opt-In Writable Field | Unit |
| [01-02](../acceptance-criteria/01-02-dockerconfig-worksize-default.md) | DockerConfig Gains a WorkSize Tunable with a Sane Default | Unit |
| [01-03](../acceptance-criteria/01-03-zero-behavior-change-for-existing-callers.md) | Zero Behavior Change for Existing `--exec` Callers and Test Suite | Integration |

## Original Criteria Overview

1. `RunSpec` (`internal/sandbox/sandbox.go`) declares a new `Writable bool` field with a doc comment describing its effect (a writable `/work` overlay is layered in when true), defaulting to `false` via Go's zero value with no constructor change required.
2. `DockerConfig` (`internal/sandbox/docker.go`) declares a new `WorkSize string` field alongside `ScratchSize`, and `DefaultDockerConfig()` sets it to a sane default sized for a full source-tree copy (larger than `ScratchSize`'s 64m build-cache default), following the exact same field-plus-default pattern.
3. No other file changes: `dockerRunArgs`, `Run`, `RunSpec.validate()`, and both `--exec` call sites in `internal/tools/exec_tools.go` are untouched, and the full existing `internal/sandbox` test suite passes unmodified, proving zero behavior change for every current caller.

## Technical Considerations

- **Implementation Notes:** Add `Writable bool` to the `RunSpec` struct (`internal/sandbox/sandbox.go`) near `SnapshotDir`, with a doc comment stating it defaults to `false` and that setting it true layers a writable `/work` tmpfs overlay over the read-only snapshot (mechanism implemented in a later story). Add `WorkSize string` to `DockerConfig` (`internal/sandbox/docker.go`) directly beside `ScratchSize`, with a matching doc comment, and set its default inside `DefaultDockerConfig()` next to `ScratchSize: "64m"` — sized larger to accommodate a full source-tree copy rather than just build caches.
- **Integration Points:** `internal/sandbox/sandbox.go` (`RunSpec`), `internal/sandbox/docker.go` (`DockerConfig`, `DefaultDockerConfig`). No integration with `dockerRunArgs`, `Run`, `internal/verify/sandboxvalidate.go`, or `internal/tools/exec_tools.go` in this story — those are downstream consumers wired up in later stories.
- **Data Requirements:** None beyond the two new struct fields; no new YAML/registry schema (`internal/registry` is explicitly out of scope for `WorkSize` per the plan's Refinement Decisions).

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A default `WorkSize` value too small for real polyglot source trees, causing tmpfs-full failures once later stories wire it into the writable mount | Medium | Size the default deliberately larger than `ScratchSize`'s 64m (e.g. large enough for a typical project source tree plus build output), documented in the field's doc comment; final tuning validated when the mount-branching story exercises real writes |
| Adding the field without a doc comment leaves future maintainers unclear on the safety contract (writable only under explicit opt-in) | Low | Doc comment on `Writable` explicitly states the `false` default and its opt-in nature, mirroring the existing `RunSpec` field doc-comment style |
| Confusing this story's scope with the mount/setup-step wiring, causing premature or duplicated work in `dockerRunArgs` | Low | Story explicitly scoped to data-model only in Constraints and Technical Considerations; `dockerRunArgs`/`Run` changes are called out as out of scope |

---

**Created:** July 21, 2026
**Status:** Acceptance Criteria Complete

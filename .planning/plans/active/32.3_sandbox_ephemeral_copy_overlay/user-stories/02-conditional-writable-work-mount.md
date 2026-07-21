# User Story 2: Conditional Writable /work Mount

**Plan:** [32.3: Sandbox Writable Overlay for Polyglot Auto-Fix](../plan.md)

## User Story

**As a** Go engineer maintaining the atcr sandbox internals (`internal/sandbox/docker.go`)
**I want** `dockerRunArgs` to mount the snapshot read-only at `/src` and add a writable `/work` tmpfs (sized by `cfg.WorkSize`) only when `spec.Writable` is true, while leaving the `Writable:false` argv byte-identical to today
**So that** `--auto-fix`'s sandboxed validation can write into its working directory (`npm run build` → `dist/`, `cargo build` → `target/`, Python `__pycache__`) without weakening the read-only guarantee `--exec` and Preflight depend on

## Story Context

- **Background:** `dockerRunArgs` (`internal/sandbox/docker.go:110`) is the single pure choke point building both the mount list and the argv for every `docker run` invocation; it already sets a global `--read-only` rootfs flag (`docker.go:117`) and today unconditionally mounts `-v spec.SnapshotDir:/work:ro` (`docker.go:140`). The only existing writable location is `--tmpfs /scratch:rw,exec,size=<cfg.ScratchSize>` (`docker.go:129`) — under `docker run --read-only`, a writable path must be added explicitly via `--tmpfs`; renaming a bind-mount's target path alone leaves that path unbacked on the read-only rootfs and any write to it still fails with `EROFS`. Story 1 added `RunSpec.Writable` (`sandbox.go`) and `DockerConfig.WorkSize` (`docker.go`, mirroring `ScratchSize`) as inert fields; this story is the first to branch on them.
- **Assumptions:** Story 1 has landed, so `spec.Writable bool` and `cfg.WorkSize string` already exist and default to `false` / a sane size respectively. `spec.validate()` (`sandbox.go:43-63`, unchanged by this story) still rejects `:` in `SnapshotDir` and still requires an absolute path, and those checks apply identically regardless of whether the mount target is `/work` or `/src`. Docker `--tmpfs <path>:rw,exec,size=<n>` mounts are ephemeral, memory-backed, and removed with the container (per `docs/docker-tmpfs-and-read-only-mounts.md`) — the exact mechanism already proven by the `/scratch` mount this story mirrors for `/work`.
- **Constraints:** `TestDockerRunArgs_HardeningFlagsPresent` (`sandbox_test.go:35`, assertion at `:55`) asserts the joined argv contains the literal `/tmp/snap:/work:ro` and MUST stay green **unmodified** — this is the regression anchor pinning `Writable:false` to today's exact mount and argv. Preflight (`docker.go:281-316`) builds its trivial-run argv via `dockerRunArgs` with `RunSpec{Command: []string{"true"}, SnapshotDir: tmpDir}`, leaving `Writable` at its zero value; Preflight's argv is a control group and must also stay byte-identical. No existing hardening flag (`--network none`, `--read-only`, `--cap-drop ALL`, `--security-opt no-new-privileges`, `--user`, resource caps) may be weakened, reordered, or made conditional by this change — only the mount-list branch and setup-step injection are new. This story does not touch `--auto-fix`'s call site (`RunSandboxedValidation` setting `Writable: true`) — that is a later story in this plan.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Opt-In Writable Configuration Surface) — requires `RunSpec.Writable` and `DockerConfig.WorkSize` to already exist |

## Success Criteria (SMART Format)

- **Specific:** `dockerRunArgs` branches on `spec.Writable`: when `false`, the mount list is exactly `-v spec.SnapshotDir:/work:ro` (unchanged); when `true`, the mount list instead binds `-v spec.SnapshotDir:/src:ro` and adds `--tmpfs /work:rw,exec,size=<cfg.WorkSize>`, and `Run` injects a `cp -a /src/. /work/ && cd /work` setup step ahead of the real payload so the writable copy is populated before the caller's command or script executes.
- **Measurable:** `TestDockerRunArgs_HardeningFlagsPresent` passes unmodified (no edit to its assertions); a new table-driven test (or new cases in the same test) asserts the `Writable:true` argv contains `/src:ro` and `--tmpfs /work:rw,exec,size=` while asserting it does NOT contain the old `/work:ro` bind form; `go build ./... ` and `go test ./internal/sandbox/...` both pass with zero new failures.
- **Achievable:** The branch is scoped to the mount-list construction in `dockerRunArgs` plus the setup-step prepend in `Run`/the script-vs-command dispatch already in `dockerRunArgs` — no new files, no change to `validate()`, no change to any other hardening flag.
- **Relevant:** This is the mechanism that actually unlocks `--auto-fix` for Node/Rust/Python ecosystems — without a real writable-backed `/work`, the opt-in flag from Story 1 has nothing to attach to and `EROFS` persists.
- **Time-bound:** Deliverable within this story's implementation session as a single self-contained, test-verified change to `internal/sandbox/docker.go` and `internal/sandbox/docker_test.go`/`sandbox_test.go`.

## Acceptance Criteria Overview

1. When `spec.Writable` is `false` (including Preflight's control-group call and every existing `--exec` call site), `dockerRunArgs`'s output argv and mount list are byte-identical to pre-story behavior — `TestDockerRunArgs_HardeningFlagsPresent`'s `/tmp/snap:/work:ro` assertion passes with no test edits.
2. When `spec.Writable` is `true`, the built argv mounts `spec.SnapshotDir` read-only at `/src` and adds `--tmpfs /work:rw,exec,size=<cfg.WorkSize>`, giving `/work` real writable backing under the container's existing `--read-only` rootfs flag.
3. When `spec.Writable` is `true`, a `cp -a /src/. /work/ && cd /work` setup step runs before the caller's real command/script, so a validation tool that writes into its working directory (e.g. `npm run build` → `dist/`) succeeds instead of failing with `EROFS`, while `/src` itself remains read-only for the container's entire lifetime and no host file under `SnapshotDir` is ever mutated.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`_

## Technical Considerations

- **Implementation Notes:** Branch the mount-list construction inside `dockerRunArgs` (`docker.go:110-150`) on `spec.Writable`, keeping the `false` path's slice construction textually unchanged so the regression anchor test cannot drift. For the `true` path, replace the trailing `-v spec.SnapshotDir:/work:ro` line with `-v spec.SnapshotDir:/src:ro` plus a new `--tmpfs /work:rw,exec,size=" + cfg.WorkSize` entry, mirroring the existing `--tmpfs /scratch:rw,exec,size=` pattern at `docker.go:129` exactly (flag ordering, `rw,exec,size=` suffix). The `cp -a /src/. /work/ && cd /work` setup step must be injected at the `dockerRunArgs`/`Run` layer — per the plan, explicitly NOT in `renderCommand` (`docker.go:153-158`), which is display-only evidence formatting and must keep showing the caller's original command/script, not the internal setup step. For the `Script != ""` path, prepend the setup step as an additional shell line ahead of the script body fed over stdin; for the `Command` (argv) path, the setup step implies wrapping in a `/bin/sh -c` invocation (since `cp && cd` cannot be spliced into a bare argv without a shell) — this wrapping only applies when `spec.Writable` is true, so the `Writable:false` argv path (still `cfg.Image` + `spec.Command...` verbatim) is unaffected.
- **Integration Points:** `internal/sandbox/docker.go` (`dockerRunArgs`, `Run`); test coverage in `internal/sandbox/sandbox_test.go` (existing `TestDockerRunArgs_HardeningFlagsPresent`, new writable-path cases) and/or `internal/sandbox/docker_test.go`. No change to `internal/sandbox/sandbox.go`'s `RunSpec`/`validate()` — those are Story 1's surface, consumed but not modified here. No change to `--exec` call sites (`internal/tools/exec_tools.go:178,215`) or `--auto-fix`'s `RunSandboxedValidation` — those are separate stories.
- **Data Requirements:** None beyond the existing `RunSpec.Writable` and `DockerConfig.WorkSize` fields introduced by Story 1; no new configuration surface, no new registry YAML knob.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| A careless edit to the `Writable:false` branch of `dockerRunArgs` silently changes the default-path mount or argv, weakening the read-only guarantee `--exec` relies on. | High | Keep the `false`-path code path textually untouched and rely on `TestDockerRunArgs_HardeningFlagsPresent` (unmodified) plus the Preflight control-group call as regression anchors; do not refactor shared argv-building logic in this story beyond what the branch strictly requires. |
| Wrapping the `Command` argv path in `/bin/sh -c` to splice in the `cp -a` setup step reintroduces a shell-interpolation surface for `spec.Command`, undermining the "no shell interpolation" guarantee documented for `RunSpec.Command`. | Medium | Only wrap when `spec.Writable` is true (a narrower, opt-in surface than the general case); pass the setup step and the original command as distinct, non-interpolated shell tokens (e.g. via `exec "$@"` after `--`) rather than string-concatenating caller-controlled command text into the shell source. |
| `cfg.WorkSize` is unset or malformed for a caller that sets `Writable:true` without picking up Story 1's `DefaultDockerConfig()` default, producing a `--tmpfs` flag with an empty or invalid `size=` value and a confusing Docker CLI error at run time. | Low | Rely on Story 1's `DefaultDockerConfig()` populating a sane `WorkSize` default (mirroring `ScratchSize`); this story does not add new validation, consistent with Story 1's documented decision not to add a registry-level validation layer for `WorkSize`. |

---

**Created:** July 21, 2026
**Status:** Draft - Awaiting Acceptance Criteria

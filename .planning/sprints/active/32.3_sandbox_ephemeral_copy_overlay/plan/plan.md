# Plan 32.3: Sandbox Writable Overlay for Polyglot Auto-Fix

## Plan Overview
**Plan Type:** feature
**Last Modified:** 2026-07-20
**Plan Goal:** Unlock `--auto-fix` validation for non-Go ecosystems (Node, Rust, Python) by giving the Docker sandbox an opt-in, ephemeral writable copy of the working tree, without weakening `--exec`'s existing hard read-only-`/work` guarantee.
**Target Users:** ATCR operators running `--auto-fix` against non-Go projects; secondarily, existing `--exec` users, who must observe zero behavior change since the two features share `internal/sandbox/docker.go`.
**Framework/Technology:** Go stdlib (`os/exec`, `context`) driving the `docker` CLI (`--tmpfs`, `-v` bind mounts, global `--read-only` rootfs flag).

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 5 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`

## Feature Analysis Summary
Epic 32.0 mounts the operator's project directory read-only at `/work` inside the `--auto-fix` sandbox to protect the host. This works for Go, whose caches redirect to a writable `/scratch` tmpfs, but any `validate_command` that writes into the working tree itself — `npm run build` → `dist/`, `cargo build` → `target/`, most codegen and non-Go builders — hits `EROFS`, is indistinguishable from a genuine validation failure, and causes `--auto-fix` to silently discard a valid fix. `dockerRunArgs`/`Run` (`internal/sandbox/docker.go`) are shared with `--exec` (Epic 11.0), which documents and depends on a hard read-only-`/work` guarantee, so the fix must be strictly opt-in per `RunSpec` rather than a global mount-mode change. Two prior `/refine-epic --deep` passes on the source epic plan corrected the original design twice: `renderCommand` is display-only and cannot inject anything into real execution, and `/work` has no writable backing under the container's global `--read-only` rootfs flag without an explicit new `--tmpfs` mount — both are reflected in the corrected Proposed Solution captured in original-requirements.md.

## Technical Planning Notes
- `dockerRunArgs` (`internal/sandbox/docker.go:110`) is the single pure choke point for both the mount-list and the argv/stdin construction — the new `Writable` branch belongs there, not in `renderCommand` (display-only, confirmed by two independent refinement passes and a prior Epic 11.0 code review).
- `--auto-fix`'s validation path (`RunSandboxedValidation`, `internal/verify/sandboxvalidate.go:43`) exclusively constructs `RunSpec{Command: argv}` — Command mode has **no shell at all** today (`args = append(args, cfg.Image); args = append(args, spec.Command...)`), so the ephemeral-copy setup step requires an explicit shell wrap (`/bin/sh -c '... && exec "$@"' -- <command...>`) for Command mode, and a prepended script body for Script mode.
- The container's rootfs runs under a global `--read-only` flag; the only currently-writable location is an explicit `--tmpfs` mount (mirrored today by `/scratch`). `Writable:true` needs a **new** `--tmpfs /work:rw,exec,size=<WorkSize>` mount, not a bare rename of the existing `-v` bind target.
- `TestDockerRunArgs_HardeningFlagsPresent` (`internal/sandbox/sandbox_test.go:55`) pins today's `/tmp/snap:/work:ro` mount — this is the regression anchor proving `Writable:false` (the default, used by every existing `--exec` call site) stays byte-identical.
- `docs/auto-fix.md` states the "mount mode is still read-only" claim in two places relative to the EROFS blockquote, and `internal/verify/autofix_exec.go`'s `ResolveAutoFixSandbox` doc comment duplicates the same stale limitation — both need updating together.

## Implementation Strategy
Add an opt-in `RunSpec.Writable bool` (default `false`) and a `DockerConfig.WorkSize string` with a sane default in `DefaultDockerConfig()` (mirroring the existing `ScratchSize` pattern, sized for a full source-tree copy). Branch `dockerRunArgs` on `spec.Writable`: when true, mount `SnapshotDir` read-only at `/src` and add a new `--tmpfs /work:rw,exec,size=<WorkSize>` mount; when false, the mount is unchanged from today. For the setup step, wrap Command-mode argv in an explicit shell invocation (`/bin/sh -c 'cp -a /src/. /work/ && cd /work && exec "$@"' -- <command...>`) and prepend the copy step to Script-mode bodies before they are streamed to `cmd.Stdin`. `RunSandboxedValidation` opts in by setting `Writable: true`; `--exec`'s two call sites in `internal/tools/exec_tools.go` are left untouched, so `Writable` stays `false` there and their documented read-only guarantee (`docs/execution.md`, the `internal/sandbox` package doc) requires no code or doc change. Regression tests prove `Writable:false` output is byte-identical to pre-epic behavior; new tests prove a mock validation script can write under `/work` when `Writable:true`, for both RunSpec modes.

## Recommended Packages
No high-ROI packages identified — this is pure Go stdlib argv/mount-flag construction and `docker` CLI invocation; `testify` (already a project dependency) covers the new tests.

## User Story Themes

1. **Opt-in configuration surface** — Add `RunSpec.Writable` and `DockerConfig.WorkSize` with safe zero-value/default behavior, proven to leave every existing caller (both `--exec` call sites, the full existing `internal/sandbox` test suite) unaffected.
2. **Conditional writable mount** — When `Writable` is true, `dockerRunArgs` mounts the snapshot read-only at `/src` and adds a writable `/work` tmpfs sized by `WorkSize`, under the container's existing `--read-only` rootfs and full hardening flag set.
3. **Ephemeral-copy setup injection** — The real payload (Command-mode argv, or Script-mode script body) is preceded by a `cp -a /src/. /work/ && cd /work` setup step, executed via an explicit shell wrap for Command mode and a prepended script line for Script mode — implemented at the actual execution layer (`dockerRunArgs`/`Run`), not the display-only `renderCommand`.
4. **`--auto-fix` opts in** — `RunSandboxedValidation` sets `Writable: true`, so non-Go `validate_command`s (npm, cargo, python, etc.) that write into the working tree succeed instead of producing a false-negative `EROFS` failure and a silently-discarded PR.
5. **Regression proof + docs parity** — Unit tests pin `Writable:false` as byte-identical to today's mount/argv (protecting `--exec`, keeping `TestDockerRunArgs_HardeningFlagsPresent` green unmodified), fakeDocker-based tests prove a mock validation script can actually write a file under `/work` when `Writable:true`, and `docs/auto-fix.md` plus the duplicate `internal/verify/autofix_exec.go` doc comment are updated to drop the stale "effectively Go-only" claim.

## Planning Success Criteria
- `RunSpec.Writable` defaults to `false`; every existing caller (both `--exec` call sites, the full `internal/sandbox` test suite including `TestDockerRunArgs_HardeningFlagsPresent`'s `/work:ro` assertion) is provably unaffected.
- When `Writable` is true, a validation command or script that writes into its working directory succeeds instead of failing with `EROFS`, proven for both Command-mode and Script-mode `RunSpec`s.
- `--auto-fix` validation (`RunSandboxedValidation`) sets `Writable: true`, eliminating the false-negative validation failure / silently-discarded-PR bug for non-Go `validate_command`s.
- The container's read-only snapshot (`/src` when `Writable` is true) never becomes writable; only the ephemeral `/work` tmpfs copy is writable, and it dies with the container — no host file is ever mutated.
- `docs/auto-fix.md`'s EROFS limitation note and adjacent "mount mode is still read-only" claim, plus `internal/verify/autofix_exec.go`'s duplicate doc comment, are updated to match the new opt-in behavior.

## Risk Mitigation
- **Risk:** Shell-wrapping Command mode could reintroduce a shell-injection surface if `spec.Command` values were ever string-concatenated into a `-c` script. **Mitigation:** pass the real argv via `-- "$@"` positional expansion inside the wrapping shell invocation (never interpolated into the script text), preserving the existing no-interpolation invariant documented in `Run`.
- **Risk:** An undersized `WorkSize` truncates `cp -a`, producing a failure indistinguishable from a genuine validation error. **Mitigation:** default `WorkSize` generously above typical repo footprints and document the config knob so operators with larger trees can raise it.
- **Risk:** Widening the shared `internal/sandbox` mount/argv logic risks an accidental regression in `--exec`'s documented read-only guarantee. **Mitigation:** `Writable` is strictly opt-in (zero value `false`), and Task 5's regression test asserts `Writable:false` output stays byte-identical to pre-epic `dockerRunArgs` output.

## Next Steps
1. `/find-documentation @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
2. `/create-documentation @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
3. `/create-user-stories @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
4. `/create-acceptance-criteria @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
5. `/design-sprint @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`
6. `/create-sprint @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`

## Refinement Decisions (2026-07-21)

Follow-ups from the `/refine-plan`-style pass that touched `codebase-discovery.json`:

- **`WorkSize` is a code-only default** (decided) — mirrors `ScratchSize`'s current treatment: a constant inside `DefaultDockerConfig()`, no new `registry.SandboxConfig` field, no `ResolveAutoFixSandbox` mapping, and no `internal/registry` component touch. If a future operator needs a larger cap than the default, that is a separate follow-up plan, not part of this one's scope.
- **T6's docs scope explicitly includes `docs/auto-fix.md:55-60`** — the paragraph immediately above the previously-cited EROFS blockquote (lines 62-70) and the "mount mode is still read-only" line (47) also states the read-only-`/work` limitation and must be updated in the same pass, not just the two ranges named in the original task list.
- **T6's docs scope also covers the image-requirement caveat** — Command-mode `Writable:true` now wraps execution in `/bin/sh -c '... && exec "$@"'`, so the operator's validation image must provide `/bin/sh` and `cp -a` (true for `alpine`/`golang`-family images, false for `distroless`). Note this constraint in `docs/auto-fix.md`'s sandbox-requirements section.
- Informational-only findings from the same pass (`test_patterns.test_location`, `test_patterns.example_test` in `codebase-discovery.json`) were reviewed and require no change — both already point at real, confirmed files.

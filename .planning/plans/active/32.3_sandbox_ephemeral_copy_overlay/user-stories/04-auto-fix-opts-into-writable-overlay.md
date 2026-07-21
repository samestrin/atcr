# User Story 4: `--auto-fix` Opts Into the Writable Overlay

**Plan:** [32.3: Sandbox Writable Overlay for Polyglot Auto-Fix](../plan.md)

## User Story

**As an** ATCR operator running `--auto-fix` against a non-Go project (Node, Rust, Python)
**I want** the sandboxed validation step to run with a writable working directory instead of the read-only `/work` mount
**So that** `npm run build` writing to `dist/`, `cargo build` writing to `target/`, or Python writing `__pycache__` next to source succeeds instead of failing with `EROFS`, so my fix is validated correctly instead of being silently discarded

## Story Context

- **Background:** `RunSandboxedValidation` (`internal/verify/sandboxvalidate.go:43`) is the `--auto-fix` validation entry point. It constructs `sandbox.RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` exclusively in Command mode (`sandboxvalidate.go:62-66`) — Script is never populated on this path, an invariant already pinned by `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` (`sandboxvalidate_test.go:58-77`), which asserts `fb.gotSpec.Script` is empty (`:71`). Stories 1-3 built the mechanism this story activates: Story 1 added `RunSpec.Writable` as an inert opt-in field; Story 2 made `dockerRunArgs`/`Run` branch on it to mount the snapshot read-only at `/src` and back `/work` with a writable `--tmpfs`, copying `/src` into `/work` before the real command runs; Story 3 covers the operator-facing config/CLI surface. This story is the one-line wiring that actually reaches the affected code path: setting `Writable: true` on the `RunSpec` this function already builds. Today `RunSandboxedValidation` never sets `Writable`, so it defaults to `false` (a read-only `/work`, per `docs/docker-tmpfs-and-read-only-mounts.md`), which is the exact `EROFS` bug this plan exists to fix — the validator crashes, the fix is assumed invalid, and the PR is silently discarded.
- **Assumptions:** Stories 1-3 have landed, so `RunSpec.Writable` and `DockerConfig.WorkSize` exist, `dockerRunArgs` correctly branches on `spec.Writable`, and the writable-overlay mechanism (`/src` read-only + `/work` tmpfs + `cp -a` setup step) is proven functional by Story 2's own tests. This story does not re-verify the overlay mechanism itself — it only verifies that `RunSandboxedValidation` requests it.
- **Constraints:** `RunSandboxedValidation` is a pure Command-mode caller — `Script` must remain empty, per the existing pinned invariant at `sandboxvalidate_test.go:71`; this story must not disturb that assertion. `--exec`'s call sites (`internal/tools/exec_tools.go` — `runTestsHandler` and `runScriptHandler`, both calling `d.runInSandbox` around line 160/178/215) construct `RunSpec{...}` without ever setting `Writable`, so they silently keep the zero value (`false`) once the field exists; this story makes **no changes** there, preserving `--exec`'s documented read-only guarantee with zero code or doc changes on that path. `ResolveAutoFixSandbox`'s Preflight call (`internal/verify/autofix_exec.go:65`, trivial-run `RunSpec{Command: []string{"true"}}`) also never sets `Writable` and must continue defaulting to `false` — a separate control group proving the writable-mount branch does not leak into the preflight path.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | S |
| **Dependencies** | Stories 1, 2, 3 (Opt-In Writable Configuration Surface; Conditional Writable /work Mount; operator-facing config/CLI surface) — requires `RunSpec.Writable` to exist and `dockerRunArgs` to correctly act on it |

## Success Criteria (SMART Format)

- **Specific:** `RunSandboxedValidation` (`internal/verify/sandboxvalidate.go:62-66`) sets `Writable: true` on the `sandbox.RunSpec` it constructs, so every `--auto-fix` validation dispatch requests the writable overlay; `--exec`'s call sites and `ResolveAutoFixSandbox`'s Preflight call remain unmodified and continue defaulting to `Writable: false`.
- **Measurable:** `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` (`sandboxvalidate_test.go:58`) gains a `assert.True(t, fb.gotSpec.Writable, ...)` assertion alongside its existing Command/SnapshotDir/Timeout/Script checks, closing the integration-gap flagged in discovery ("Task 4 has no pinning test site"); a non-Go validation command that writes into its working directory (e.g. a script running `mkdir dist && touch dist/out`) passes under the sandboxed path where it previously failed with `EROFS`; `go build ./...` and `go test ./internal/verify/...` pass with zero new failures.
- **Achievable:** The change is a single field addition (`Writable: true`) to an existing struct literal in `sandboxvalidate.go`, with a single new assertion in an existing test — no new files, no new exported surface, no change to `translateRunResult` or the pre-flight guards.
- **Relevant:** This is the payoff story — the point where Stories 1-3's mechanism actually reaches `--auto-fix`'s real code path and fixes the user-visible bug: non-Go `validate_command`s no longer produce a false-negative `EROFS` failure and a silently-discarded PR.
- **Time-bound:** Deliverable within this story's implementation session as a single self-contained, test-verified change to `internal/verify/sandboxvalidate.go` and `internal/verify/sandboxvalidate_test.go`.

## Acceptance Criteria Overview

1. `RunSandboxedValidation` sets `Writable: true` on the `sandbox.RunSpec` it constructs for every dispatch — `Script` remains empty per the existing pinned invariant, and the pre-flight guards (empty argv, missing dir) still short-circuit before any `Backend.Run` call.
2. `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` is extended with an assertion that `fb.gotSpec.Writable` is `true`, so a future refactor cannot silently drop the field without a failing test.
3. `--exec`'s call sites (`exec_tools.go`) and `ResolveAutoFixSandbox`'s Preflight `RunSpec` remain textually unchanged and continue to default to `Writable: false`, verified by inspection/existing tests showing no regression in `--exec`'s read-only behavior.

_Detailed AC: `/create-acceptance-criteria @.planning/plans/active/32.3_sandbox_ephemeral_copy_overlay/`_

## Technical Considerations

- **Implementation Notes:** Add `Writable: true` to the `sandbox.RunSpec{Command: argv, Timeout: timeout, SnapshotDir: dir}` literal at `sandboxvalidate.go:62-66`. No other logic in `RunSandboxedValidation` changes: the empty-argv and missing-dir guards (`:44-60`) run unchanged before the spec is built, and `translateRunResult` (`:100-117`) is untouched since it only maps `sandbox.RunResult` back to `ValidationResult` and has no knowledge of `Writable`.
- **Integration Points:** `internal/verify/sandboxvalidate.go` (`RunSandboxedValidation`); test coverage in `internal/verify/sandboxvalidate_test.go` (extend `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec`). No change to `internal/tools/exec_tools.go` or `internal/verify/autofix_exec.go` — both are explicit control groups this story must leave byte-identical.
- **Data Requirements:** None. No new configuration surface; consumes only the `RunSpec.Writable` field Story 1 already introduced.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| The one-line `Writable: true` change ships without a corresponding test assertion, so a future refactor could silently drop it and reintroduce the `EROFS` false-negative with no CI signal. | Medium | Extend the existing `TestRunSandboxedValidation_RoutesThroughBackendWithBuiltSpec` pin site with an explicit `fb.gotSpec.Writable` assertion in this same story, per AC 2 — do not defer test coverage to a later story. |
| A copy/paste of the change accidentally touches `--exec`'s call sites or `ResolveAutoFixSandbox`'s Preflight `RunSpec`, weakening their read-only guarantee without any test catching it (neither currently asserts `Writable`). | High | Scope the diff strictly to `sandboxvalidate.go`'s one struct literal; treat `exec_tools.go` and `autofix_exec.go` as read-only control groups for this story and verify via `git diff` that neither file changes before commit. |
| If Story 2's writable-overlay mechanism has a latent bug (e.g. `cp -a` setup step or `/src`/`/work` split not working correctly), this story's `Writable: true` flip is the first place that bug becomes user-visible in the `--auto-fix` path. | Low | This story depends on Stories 1-3 already being test-verified independently; if Story 2's own tests pass, this story only needs to confirm the flag is correctly threaded through, not re-validate the underlying mount mechanism. |

---

**Created:** July 21, 2026
**Status:** Draft - Awaiting Acceptance Criteria

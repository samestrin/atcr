# User Story 2: Sandbox Resolution and Preflight Gate for Auto-Fix

**Plan:** [32.0: Sandboxed Auto-Fix Validation](../plan.md)

## User Story

**As an** ATCR maintainer or CI/CD operator running `atcr review --auto-fix`
**I want** the auto-fix CLI gate to build and preflight-check a `sandbox.Backend` from my project's configuration before any patch is applied
**So that** `--auto-fix` refuses to run — before touching a single file — whenever sandboxed validation is expected but Docker or the configured backend is unavailable or misconfigured, instead of silently falling back to running untrusted, LLM-generated code directly on my host or CI runner

## Story Context

- **Background:** Epic 11.0 already built `internal/sandbox.Backend`/`DockerBackend` and the resolver pattern `internal/verify.ResolveExecBackend` (`internal/verify/exec.go:24-57`) that turns a `*registry.SandboxConfig` into a preflight-checked, ready-to-run Docker backend for the `--exec` reviewer-reproduction feature. `cmd/atcr/autofix.go:validateAutoFixBackend` (`cmd/atcr/autofix.go:107`) is the single all-or-nothing gate for `--auto-fix`: it already resolves the apply target, the validation command/timeout, and GitHub credentials in one pass, collecting every problem into one `missing []string` and returning a single usage error (exit 2) that names all of them. This story adds sandbox backend resolution as the fourth piece of that same gate, giving `runAutoFix`/`orchestrateAutoFix` a ready `sandbox.Backend` to consume — the actual routing of the validation command through that backend's `Run` method is Story 1's scope, and the `--no-sandbox` opt-out flag itself is Story 3's scope. This story is strictly about building the backend and gating on its preflight result.
- **Assumptions:**
  - `internal/sandbox.Backend`, `DockerBackend`, `NewDockerBackend`, `DefaultDockerConfig`, and `Preflight` are stable, already-shipped APIs from Epic 11.0 and require no changes.
  - `proj.Sandbox` (`*registry.SandboxConfig`, `internal/registry/project.go:85`) is already in scope at the gate call site (`cmd/atcr/review.go:353` already passes `cfg.Project`), so no new config plumbing is needed to read it.
  - Unlike `--exec` (sandboxing off by default, opt-in via a flag), auto-fix's default posture inverts: sandboxing is ON by default for the validation step, and only an explicit override (Story 3's `--no-sandbox`) turns it off. This story's resolver must expose that inverted default cleanly (e.g. an `enabled bool` parameter analogous to `execEnabled`, defaulting to `true` for auto-fix call sites) so Story 3 can wire `--no-sandbox` into it without reshaping the function signature.
- **Constraints:**
  - No new sandbox backend, Docker SDK, or third-party execution dependency — this is integration-only, reusing `internal/sandbox` exactly as built.
  - Must preserve the existing all-or-nothing gate contract: sandbox resolution failures join the same `missing []string` collection in `validateAutoFixBackend`, not a separate early-return error path.
  - Must preserve the refuse-on-preflight-failure discipline from `ResolveExecBackend`: any preflight error is a hard error with no silent fallback to unsandboxed execution.
  - `registry.SandboxConfig.Validate()` (`internal/registry/sandbox.go:43-74`) currently requires both `Image` and `TestCommand` unconditionally — a `--exec`-only assumption. Reusing it unmodified for auto-fix would force every `--auto-fix` operator to configure `test_command` even when they never use `--exec`. This story must surface that tension as an explicit open design question (not silently resolve it by, e.g., quietly relaxing validation in a way that weakens `--exec`'s existing guarantees).
  - Must not modify `internal/verify/exec.go` or alter `--exec`'s existing behavior in any way — the new resolver ships as a sibling file (`internal/verify/autofix_exec.go`), so Epic 11.0's feature remains byte-for-byte unaffected and the existing `internal/verify/exec_test.go` suite (`TestResolveExecBackend_*`, `exec_test.go:25-65`) keeps passing unedited (plan.md Planning Success Criteria; pinned by AC 02-02 Edge Case 4).

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | None within this plan (builds on the already-shipped Epic 11.0 `internal/sandbox` package and `internal/verify.ResolveExecBackend` pattern). Story 1 (routing validation through the resolved backend's `Run`) and Story 3 (`--no-sandbox` flag) both consume the resolver this story produces. |

## Success Criteria (SMART Format)

- **Specific:** A new resolver function (e.g. `verify.ResolveAutoFixSandbox`, mirroring `ResolveExecBackend`'s signature and structure) builds a `sandbox.DockerBackend` from `*registry.SandboxConfig` and an enabled/disabled posture, runs `Preflight(ctx)`, and returns either a ready `sandbox.Backend` or a wrapped error — with zero new sandbox execution logic beyond what Epic 11.0 already ships.
- **Measurable:** `validateAutoFixBackend` calls the new resolver as a fourth checked piece; any preflight or configuration failure appends to the same `missing []string` slice and surfaces in the single combined usage error (exit 2), verified by a table-driven test asserting the combined error text names the sandbox failure alongside any other missing piece in the same run.
- **Achievable:** The resolver is a close structural mirror of `ResolveExecBackend` (24-57 lines in the reference implementation), reusing every `sandbox` and `registry` type as-is; no new package or external dependency is introduced.
- **Relevant:** Directly satisfies the plan's acceptance criterion that `--auto-fix` refuses to run (fail-closed) when sandboxing is expected but unavailable/misconfigured — the load-bearing safety guarantee of this epic.
- **Time-bound:** Implementable and hermetically tested (via the `fakeDocker` shim pattern from `internal/verify/exec_test.go`, no live Docker daemon required) within this sprint's first phase, ahead of Story 1's routing work which consumes the resolved backend.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-resolver-builds-and-preflights-sandbox-backend.md) | Resolver Builds and Preflights a Sandbox Backend | Unit |
| [02-02](../acceptance-criteria/02-02-inverted-default-posture-and-validation-tension.md) | Inverted Default Posture (Sandbox-On-By-Default) and the SandboxConfig.Validate() Tension | Unit |
| [02-03](../acceptance-criteria/02-03-gate-integration-and-combined-error.md) | Gate Integration — Sandbox Resolution as the Fourth Piece of validateAutoFixBackend | Integration |

## Original Criteria Overview

1. A new resolver in `internal/verify` builds a `sandbox.Backend` from `*registry.SandboxConfig`, applying the same field-override pattern as `ResolveExecBackend` (DockerPath, Image, Memory, CPUs, PidsLimit, TimeoutSecs), and runs `Preflight(ctx)` before returning it as ready.
2. The resolver's default posture is inverted from `ResolveExecBackend`: sandboxing is expected/on by default for auto-fix, so an unconfigured or failing backend is a hard refusal unless an explicit disable signal (the future `--no-sandbox`) is passed in — never a silent fallback to unsandboxed execution.
3. `validateAutoFixBackend` calls the new resolver as a fourth piece of its existing all-or-nothing gate, joining any resolution/preflight failure into the same combined `missing []string` usage error alongside apply-target, validation-command, and GitHub-credential failures, and the resolved backend rides the `autoFixBackend` struct into `runAutoFix` without re-resolution.

## Technical Considerations

- **Implementation Notes:**
  - Add `internal/verify/autofix_exec.go` with a resolver mirroring `ResolveExecBackend` (`internal/verify/exec.go:24-57`): construct `sandbox.DefaultDockerConfig()`, override fields only when set on `*registry.SandboxConfig` (DockerPath, Image, Memory, CPUs, PidsLimit, TimeoutSecs), construct via `sandbox.NewDockerBackend(cfg)`, call `backend.Preflight(ctx)`, wrap any error, and return the ready `sandbox.Backend`.
  - Unlike `ResolveExecBackend` (`execEnabled=false` → `(nil,nil,0,nil)`, off by default), this resolver's disabled path is the exception, not the default — its signature should make the "sandboxing is expected unless explicitly disabled" posture explicit and easy for Story 3 to wire `--no-sandbox` into without changing the call shape `validateAutoFixBackend` already uses for its other three checks.
  - Extend `autoFixBackend` (`cmd/atcr/autofix.go:59`) with a new field to carry the resolved `sandbox.Backend` through to `runAutoFix`, so Story 1's routing work reads it directly rather than re-resolving.
  - Add the resolver call inside `validateAutoFixBackend` (`cmd/atcr/autofix.go:107`) as step (4), following the existing pattern at steps (1)-(3): append to `missing` on failure, assign to `be.<field>` on success.
  - Timeout precedence (open design question, from `documentation/autofix-gate-and-config.md`): mirroring `ResolveExecBackend` means applying `sc.TimeoutSecs` to `cfg.Timeout`, but that value is only the *backend default* used when `RunSpec.Timeout == 0`. Story 1's dispatch always forwards the auto-fix validation timeout (`auto_fix.validate_timeout`, default 2m) into `RunSpec.Timeout` (AC 01-01 Scenario 3), so `RunSpec.Timeout` must remain the authoritative per-run budget — design must ensure a configured `sandbox.timeout_secs` (default 60s) never silently shrinks the operator's validation budget.
- **Integration Points:**
  - `internal/sandbox.Backend`, `DockerBackend`, `NewDockerBackend`, `DefaultDockerConfig` (Epic 11.0, unchanged).
  - `internal/registry.SandboxConfig` (`internal/registry/sandbox.go`) — read-only consumption; whether its `Validate()` (currently requiring both `Image` and `TestCommand` unconditionally) needs to change for the auto-fix path, or whether auto-fix should validate through a parallel/relaxed path, is an open design question this story surfaces rather than resolves. Do not silently loosen `--exec`'s existing validation to accommodate auto-fix.
  - `cmd/atcr/autofix.go:validateAutoFixBackend` and `autoFixBackend` struct — the CLI-level integration point.
  - `internal/verify/exec_test.go`'s `fakeDocker` shim pattern (a POSIX shell script impersonating `docker`, injected via `DockerConfig.DockerPath`) is the intended hermetic test harness for the new resolver, avoiding any live Docker daemon dependency in tests.
- **Data Requirements:** No new persisted data or schema changes; consumes the existing `proj.Sandbox` field already loaded from `.atcr/config.yaml`'s `sandbox:` block.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| `SandboxConfig.Validate()` unconditionally requiring `Image` + `TestCommand` (a `--exec`-only assumption) would force every `--auto-fix` operator to configure a `test_command` they never use, just to satisfy config validation. | Medium | Treat as an explicit open design question for `/design-sprint`/implementation: either split validation so `TestCommand` is required only when `--exec` reads it, or introduce a parallel light validation path for the auto-fix sandbox block. Do not silently relax `--exec`'s existing contract to work around it. |
| Making sandboxing "on by default" for auto-fix means any project without a configured `[sandbox]` block will now hard-refuse `--auto-fix` at the gate — a behavior change for existing `--auto-fix` users who never needed Docker before. | High | This is the intended fail-closed posture per the epic's acceptance criteria, but it must ship together with (or immediately alongside) Story 3's `--no-sandbox` escape hatch so operators without Docker have a documented, loudly-warned way to keep running; this story should not land in isolation without that coordination being tracked. |
| Adding a new field to `autoFixBackend` before Story 1 wires actual routing through it leaves the field populated-but-unused, risking an `unused` lint/vet flag or silent drift if Story 1 lands with a different shape. | Low | Keep the field type and name aligned with what `internal/sandbox.Backend` already exposes (no wrapper type), and confirm with a test that it is populated by the gate even though `runAutoFix`'s validation call doesn't yet consume it — Story 1 closes that loop. |

---

**Created:** July 19, 2026
**Status:** Acceptance Criteria Generated

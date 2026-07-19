## Plan Overview
**Last Modified:** 2026-07-19 (refined via `/refine-plan --deep`)
**Plan Type:** feature
**Plan Goal:** Route the `--auto-fix` pipeline's post-apply validation step (`internal/verify.RunConfiguredValidation`) through the existing `internal/sandbox` container isolation (built for Epic 11.0's `--exec` feature) so that untrusted, LLM-generated code never runs directly on the host or CI runner during validation. Provide an explicit `--no-sandbox` opt-out for environments without Docker, backed by strict CLI and documentation warnings.
**Target Users:** ATCR maintainers running `--auto-fix` locally, and CI/CD pipeline operators running it unattended.
**Framework/Technology:** Go 1.25, Cobra CLI, existing `internal/sandbox.Backend`/`DockerBackend`.

## Planning Deliverables
### User Stories
- **Location:** [`user-stories/*.md`](user-stories/)
- **Status:** Generated
- **Estimated Count:** 4 stories

### Acceptance Criteria
- **Location:** [`acceptance-criteria/*.md`](acceptance-criteria/)
- **Status:** Pending - generate with `/create-acceptance-criteria @.planning/plans/active/32.0_sandbox_execution_environment/`

## Feature Analysis Summary
Epic 17.0 (Auto-Merged Fixes) already applies LLM-generated patches to the working tree and runs a configured validation command (`internal/verify.RunConfiguredValidation`) directly on the host via `os/exec`. `internal/atomicfs` reverts the filesystem on validation failure, but it does not contain what the validation *command itself* can do while it runs — a malicious or hallucinated build/test script executes with the same privileges as the `atcr` process. Epic 11.0 already solved this exact isolation problem for the unrelated `--exec` reviewer-reproduction feature: `internal/sandbox.Backend` (with `DockerBackend` as the sole implementation) runs a command in a network-isolated, resource-capped, non-root, ephemeral container, gated by `internal/verify.ResolveExecBackend`. This plan's entire scope is wiring that existing, battle-tested sandbox into the auto-fix validation call site — no new sandbox infrastructure — plus an explicit, loudly-warned `--no-sandbox` escape hatch.

## Technical Planning Notes
- `internal/verify.ResolveExecBackend` (internal/verify/exec.go) is the direct pattern to mirror: translate a `*registry.SandboxConfig` into a `sandbox.DockerConfig`, construct `sandbox.NewDockerBackend`, and require `Preflight()` to pass before returning a ready `sandbox.Backend`.
- A key semantic gap to resolve during design: `--exec`'s `RunSpec.SnapshotDir` is mounted **read-only** (the run cannot mutate the tree), but auto-fix's validation step runs **after** a patch has already been applied and must both observe and potentially write build artifacts into that same working tree. The sandboxed validation path needs read-write access to the already-mutated tree, which differs from the `--exec` mount semantics and needs explicit design attention.
- `cmd/atcr/autofix.go:validateAutoFixBackend` is the existing all-or-nothing CLI gate (collects every missing piece — apply target, validation command, GitHub credentials — into one usage error). Sandbox backend resolution and the `--no-sandbox` override should join this same gate rather than introduce a new error-handling style.
- `internal/registry/sandbox.go:SandboxConfig` currently requires both `Image` and `TestCommand` unconditionally for the `--exec` use case; reusing vs. extending this block for auto-fix's validation sandbox is an open design question for `/design-sprint`.
- No new third-party dependency is required — `internal/sandbox` shells out to the `docker` CLI directly (no Docker SDK), and this plan is pure integration work.

## Documentation References
- [CRITICAL] [Sandbox Backend Interface (Backend, RunSpec, RunResult)](documentation/sandbox-backend-interface.md)
- [CRITICAL] [DockerBackend Implementation (Run, Preflight, dockerRunArgs)](documentation/docker-backend-implementation.md)
- [CRITICAL] [Resolver Pattern — ResolveExecBackend (mirror for auto-fix)](documentation/resolver-pattern-resolveexecbackend.md)
- [CRITICAL] [Auto-Fix Gate & Config Surface (validateAutoFixBackend, SandboxConfig/AutoFixConfig, --no-* precedents)](documentation/autofix-gate-and-config.md)
- [CRITICAL] [Auto-Fix Validation Contract (RunConfiguredValidation, ValidationResult)](documentation/autofix-validation-contract.md)
- [IMPORTANT] [Sandbox Testing Patterns (fakeDocker shim, argv assertions)](documentation/sandbox-testing-patterns.md)

## Implementation Strategy
Build a new resolver (mirroring `internal/verify.ResolveExecBackend`) that `cmd/atcr/autofix.go`'s validation gate calls to obtain a ready `sandbox.Backend`, defaulting to sandboxed execution. Route `internal/verify.RunConfiguredValidation`'s command execution through `sandbox.Backend.Run` when sandboxing is enabled (falling back to today's direct `os/exec` path only when `--no-sandbox` is explicitly passed). Add the `--no-sandbox` flag to `addAutoFixFlags`, wired with strict CLI warning text and matching `docs/` security warnings modeled on the existing `docs/execution.md` security-posture section for `--exec`.

## Recommended Packages
No high-ROI packages identified — the container execution primitive (`internal/sandbox`) already exists and this plan is integration-only.

## User Story Themes
1. **Auto-fix validation defaults to sandboxed execution** (AC1) — the core wiring: `--auto-fix`'s validation step routes through `internal/sandbox` by default, with no behavior change to any other part of the pipeline (apply, revert, branch/commit/PR).
2. **Sandbox resolution and Preflight gate for auto-fix** — a new resolver (mirroring `ResolveExecBackend`) that builds and preflight-checks a `sandbox.Backend` from configuration as part of `validateAutoFixBackend`'s all-or-nothing gate, refusing `--auto-fix` when sandboxing is expected but unavailable.
3. **`--no-sandbox` opt-out flag with CLI security warnings** (AC2) — an explicit flag that bypasses the sandbox gate, printing a strict, unmissable warning that host code execution is enabled.
4. **Documentation for the security posture and `--no-sandbox` risk** — `docs/` content warning operators about running `--auto-fix` unsandboxed, mirroring `docs/execution.md`'s existing `--exec` security-posture section.

## Planning Success Criteria
- `--auto-fix`'s post-apply validation command runs inside `internal/sandbox` by default, with zero behavior change when sandboxing is unavailable and `--no-sandbox` is not passed (the run refuses, per the epic's fail-closed posture).
- A `--no-sandbox` flag exists, is off by default, and its use is accompanied by an unmissable CLI warning and matching `docs/` warning.
- No new sandbox backend or third-party execution package is introduced; `internal/sandbox.DockerBackend` is reused as-is or minimally extended.
- Existing `--exec` (Epic 11.0) and `--auto-fix` apply/revert (Epic 17.0) behavior is unaffected outside the validation call site.

## Risk Mitigation
- **Risk:** The read-only `RunSpec.SnapshotDir` mount semantics from `--exec` may not fit auto-fix's need to validate an already-mutated, writable working tree. **Mitigation:** Surface this as an explicit design question in `/design-sprint` rather than assuming read-only reuse; may require a `RunSpec` extension or a distinct mount mode.
- **Risk:** `--no-sandbox` could become a de facto default if the warning is too easy to ignore in CI logs. **Mitigation:** Model the warning on `docs/execution.md`'s existing security-posture language and require it to print to stderr on every `--no-sandbox` run, not just once.
- **Risk:** Component count (4: `internal/sandbox/`, `internal/autofix/`, `cmd/atcr/`, `docs/`) exceeds `/execute-epic`'s lightweight scope guard, confirmed by the epic's own `/refine-epic` pass — this plan correctly routes through the full sprint pipeline rather than the one-shot path.

## Next Steps
1. `/find-documentation @.planning/plans/active/32.0_sandbox_execution_environment/`
2. `/create-documentation @.planning/plans/active/32.0_sandbox_execution_environment/`
3. `/create-user-stories @.planning/plans/active/32.0_sandbox_execution_environment/`
4. `/create-acceptance-criteria @.planning/plans/active/32.0_sandbox_execution_environment/`
5. `/design-sprint @.planning/plans/active/32.0_sandbox_execution_environment/`
6. `/create-sprint @.planning/plans/active/32.0_sandbox_execution_environment/`

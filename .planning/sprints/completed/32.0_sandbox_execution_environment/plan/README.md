## Overview
Plan 32.0 hardens `--auto-fix` (Epic 17.0) by routing its post-apply validation command through the existing `internal/sandbox` container isolation (Epic 11.0) instead of running it directly on the host. This closes the gap where `atomicfs` protects files but not the machine running the validation command. Scope is wiring-only: no new sandbox backend, just resolving and gating a `sandbox.Backend` for the auto-fix validation call site, plus an explicit, loudly-warned `--no-sandbox` opt-out.

## Workflow Status
- [x] **Plan Created**
- [x] **User Stories** - `/create-user-stories @.planning/plans/active/32.0_sandbox_execution_environment/`
- [x] **Acceptance Criteria** - `/create-acceptance-criteria @.planning/plans/active/32.0_sandbox_execution_environment/`
- [x] **Design Sprint** - `/design-sprint @.planning/plans/active/32.0_sandbox_execution_environment/`
- [ ] **Sprint Plan** - `/create-sprint @.planning/plans/active/32.0_sandbox_execution_environment/`

## Timeline & Milestones
Estimate TBD (epic listed "Estimated time: TBD"). Sizing follows `/design-sprint`'s complexity scoring once user stories and acceptance criteria are drafted; Medium complexity / 4 estimated stories suggests a short (single-digit-day) sprint comparable in scope to Epic 7.1 (Local Syntax Guard).

## Resource Requirements
Core Engineering (Go, Cobra CLI, Docker-based sandbox familiarity). No new external services or credentials beyond what `--auto-fix` and `--exec` already require (a reachable Docker daemon for the sandbox backend).

## Expected Outcomes
- `--auto-fix` validation runs sandboxed by default via `internal/sandbox`.
- `--no-sandbox` flag exists for environments without Docker, with strict CLI + docs warnings.
- No regression to existing `--exec` (Epic 11.0) or auto-fix apply/revert/PR (Epic 17.0) behavior.

## Risk Summary
Primary risk is a semantic mismatch between `--exec`'s read-only snapshot mount and auto-fix's need to validate an already-mutated, writable working tree — flagged for `/design-sprint` rather than assumed away. Secondary risk is a `--no-sandbox` warning that is easy to miss in CI logs. See `plan.md` Risk Mitigation for detail.

## Documentation References
- [CRITICAL] [Sandbox Backend Interface (Backend, RunSpec, RunResult)](documentation/sandbox-backend-interface.md)
- [CRITICAL] [DockerBackend Implementation (Run, Preflight, dockerRunArgs)](documentation/docker-backend-implementation.md)
- [CRITICAL] [Resolver Pattern — ResolveExecBackend (mirror for auto-fix)](documentation/resolver-pattern-resolveexecbackend.md)
- [CRITICAL] [Auto-Fix Gate & Config Surface (validateAutoFixBackend, SandboxConfig/AutoFixConfig, --no-* precedents)](documentation/autofix-gate-and-config.md)
- [CRITICAL] [Auto-Fix Validation Contract (RunConfiguredValidation, ValidationResult)](documentation/autofix-validation-contract.md)
- [IMPORTANT] [Sandbox Testing Patterns (fakeDocker shim, argv assertions)](documentation/sandbox-testing-patterns.md)

See [documentation/README.md](documentation/README.md) for the full index.

## Plan Assets
- [Original Request](original-requirements.md)
- [Plan](plan.md)
- [Metadata](metadata.md)
- [Codebase Discovery](codebase-discovery.json)
- [User Stories](user-stories/)
- [Acceptance Criteria](acceptance-criteria/)
- [Sprint Design](sprint-design.md)

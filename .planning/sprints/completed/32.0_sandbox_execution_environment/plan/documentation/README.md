# Plan Documentation References

**Created:** July 19, 2026 11:59:29AM
**Refined:** July 19, 2026 (via `/refine-docs --deep`)
**Plan:** [../plan.md](../plan.md)
**Grounded Against:** codebase-discovery.json, .planning/specifications/

---

## Priority Legend

- **[CRITICAL]** - Must read before starting implementation
- **[IMPORTANT]** - Should review during development
- **[REFERENCE]** - Consult as needed

---

## Documentation Files

- [Sandbox Backend Interface (Backend, RunSpec, RunResult)](sandbox-backend-interface.md) — [CRITICAL]
- [DockerBackend Implementation (Run, Preflight, dockerRunArgs)](docker-backend-implementation.md) — [CRITICAL]
- [Resolver Pattern — ResolveExecBackend (mirror for auto-fix)](resolver-pattern-resolveexecbackend.md) — [CRITICAL]
- [Auto-Fix Gate & Config Surface (validateAutoFixBackend, SandboxConfig/AutoFixConfig, --no-* precedents)](autofix-gate-and-config.md) — [CRITICAL]
- [Auto-Fix Validation Contract (RunConfiguredValidation, ValidationResult)](autofix-validation-contract.md) — [CRITICAL]
- [Sandbox Testing Patterns (fakeDocker shim, argv assertions)](sandbox-testing-patterns.md) — [IMPORTANT]
- [Documentation Source Index](source.md) — [REFERENCE]

---

## Source Attribution

All documentation is grounded in:
- **Source Documents:** `internal/verify/exec.go`, `internal/verify/localvalidate.go`, `internal/sandbox/sandbox.go`, `internal/sandbox/docker.go`, `internal/sandbox/sandbox_test.go`, `internal/sandbox/docker_test.go`, `cmd/atcr/autofix.go`, `cmd/atcr/review.go`, `internal/registry/sandbox.go`, `internal/registry/autofix.go`, `internal/registry/project.go`
- **Codebase Discovery:** `.planning/plans/active/32.0_sandbox_execution_environment/codebase-discovery.json`
- **Specifications:** `.planning/specifications/` (no architectural specs matched this plan — see `source.md`)

---

## How to Use

1. Start with **Critical** documentation before coding
2. Review **Important** docs during development
3. Consult **Reference** docs for specific questions

---

**Navigation:** [← Back to Plan](../README.md)

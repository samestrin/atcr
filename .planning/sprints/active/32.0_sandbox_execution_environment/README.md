# Sprint 32.0: Sandboxed Auto-Fix Validation

**Type:** ✨ Feature
**Complexity:** 8/12 (COMPLEX)
**Timeline:** 9.5 days
**Phases:** 5
**Execution Mode:** Gated 🚧 | **Adversarial Review:** ENABLED 🎯 (inline: CRITICAL/HIGH, defer: MEDIUM/LOW)
**Status:** Active

---

## Overview

Route the `--auto-fix` pipeline's post-apply validation step (`internal/verify.RunConfiguredValidation`) through the existing `internal/sandbox` container isolation built for Epic 11.0's `--exec` feature, so LLM-generated `go build`/`npm test` commands never execute directly on the host or CI runner. Provide an explicit `--no-sandbox` opt-out, backed by strict CLI and documentation warnings, for environments without Docker.

See [sprint-plan.md](sprint-plan.md) for the full task breakdown and [metadata.md](metadata.md) for tracking.

## Timeline

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Foundation — Sandbox Resolver & Design Decisions | 2 days |
| 2 | Core — Sandbox-Routed Validation Dispatch | 2 days |
| 3 | Gate Integration & Opt-Out | 2 days |
| 4 | Integration Testing & Zero-Behavior-Change Verification | 2 days |
| 5 | Documentation & Final Validation | 1.5 days |

## Expected Outcomes

- `--auto-fix` validation runs inside `internal/sandbox` by default; fail-closed refusal when sandboxing is expected but unavailable and `--no-sandbox` was not passed.
- A `sandbox.RunResult` → `verify.ValidationResult` adapter with a documented, non-lossy translation contract.
- Sandbox resolution wired as the fourth checked piece of `validateAutoFixBackend`'s all-or-nothing gate.
- `--no-sandbox` opt-out flag with an unconditional, non-memoized stderr warning on every invocation.
- `docs/` coverage of the sandboxed-by-default posture, the `auto_fix:` config block, and the `--no-sandbox` risk.

## Risk Summary (Top 3)

1. **`SandboxConfig.Validate()`'s unconditional `Image`+`TestCommand` requirement** forces every `--auto-fix` operator to configure a `test_command` they never use. Mitigation: Phase 1 resolves this explicitly (split validation or parallel light-validation path) rather than deferring it into implementation ad hoc.
2. **Combined `sandbox.RunResult.Output` cannot be re-split into `Stdout`/`Stderr`**, risking a silent change to what operators see in failure reports. Mitigation: Phase 2's adapter documents the merge explicitly and routes combined output to `Stdout` only.
3. **Sandboxing on-by-default is a behavior change** for existing `--auto-fix` users without Docker. Mitigation: Story 3 (`--no-sandbox`) ships in the same sprint (Phase 3), not deferred to a later release.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — task-by-task execution plan
- [metadata.md](metadata.md) — tracking and execution metrics
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest
- [plan/](plan/) — original plan, sprint-design.md, user-stories/, acceptance-criteria/, documentation/

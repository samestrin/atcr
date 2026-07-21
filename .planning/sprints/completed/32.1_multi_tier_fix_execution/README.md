# Sprint 32.1: Multi-Tier Fix Execution Engine

**Type:** ✨ Feature
**Complexity:** 8/12 (COMPLEX)
**Timeline:** 10 days
**Phases:** 4
**Execution Mode:** Gated 🚧 | **Adversarial Review:** ENABLED 🎯 (inline: CRITICAL/HIGH, defer: MEDIUM/LOW)
**Status:** Active

---

## Overview

Evolve atcr's single-model fix executor into a complexity-aware, ceiling-configurable execution path. `ExecutorConfig` gains a `max_estimated_minutes` ceiling (plus an optional `max_severity_for_fix` ceiling), `generateFixes` skips-and-logs any finding beyond that ceiling instead of attempting it, and a second, independently-configured executor run picks up exactly the findings the first tier skipped — delivering a two-tier cheap-then-frontier fix workflow that routes on the `EstMinutes` signal reviewers already emit.

See [sprint-plan.md](sprint-plan.md) for the full task breakdown and [metadata.md](metadata.md) for tracking.

## Timeline

| Phase | Focus | Duration |
|-------|-------|----------|
| 1 | Foundation — Config Surface & Validation | 2 days |
| 2 | Core Routing — Skip Chain & Self-Gating | 3 days |
| 3 | Two-Tier Integration & Verification | 3 days |
| 4 | Documentation & Validation | 2 days |

## Expected Outcomes

- `ExecutorConfig` complexity-ceiling fields (`MaxEstimatedMinutes`, `MaxSeverityForFix`) with `EffectiveXxx()` resolvers and full `validateExecutor` range/cross-field validation.
- A `withinComplexityCeiling` predicate wired into `generateFixes`'s pre-dispatch skip chain, plus a self-gating decline branch, both surfaced via the existing `FixWarning` + `logPipelineWarning("executor_ceiling_skip", ...)` contract.
- An integration/E2E-verified two-tier workflow proving every finding is fixed by exactly one tier or explicitly skipped-and-logged — never both, never neither.
- Updated `docs/registry.md` / `docs/findings-format.md` and a worked two-tier example in `examples/registry-with-executor.yaml`, validated by a dry-run config load.

## Risk Summary (Top 3)

1. **Design ambiguity between single-executor+ceiling and a true multi-executor chain** — resolved in sprint-design.md's Design Decision: a single `ExecutorConfig` gains a ceiling; "tier 2" is a second, independently-configured run against the same `findings.json`. No `Registry.Executor` schema redesign in this plan.
2. **`EstMinutes` is a best-effort, model-emitted integer** (non-numeric parses as `0`) — treated as a soft routing hint layered on existing confidence/severity gates, not a correctness guarantee; explicit test coverage for the zero/unset case.
3. **Cross-tier fix-attribution edge cases** (Story 3, flagged High complexity in `plan/test-planning-matrix.md`) — dedicated integration test asserting on attribution state explicitly, not just absence of a crash.

## Sprint Assets

- [sprint-plan.md](sprint-plan.md) — task-by-task execution plan
- [metadata.md](metadata.md) — tracking and execution metrics
- [sprint-knowledge.yaml](sprint-knowledge.yaml) — knowledge manifest
- [plan/](plan/) — original plan, sprint-design.md, user-stories/, acceptance-criteria/, documentation/

# Original Requirements

**Date:** July 20, 2026 10:15:24AM
**Arguments:** `@.planning/epics/active/32.1_multi_tier_fix_execution.md`
**Target:** `.planning/epics/active/32.1_multi_tier_fix_execution.md`

## Purpose

This document captures the original, unmodified source input for this plan. It is the source of truth for scope and intent — downstream artifacts (plan.md, user stories, acceptance criteria, sprint design) must trace back to this content and should not silently drift from it.

This plan originated as Epic Plan 32.1, which was submitted to `/execute-epic` and failed its scope guard (≤6 tasks / ≤2 components). A `/refine-epic` pass on 2026-07-17 confirmed the violation (5 tasks / 4 components) and recommended routing through `/init-plan` for the full sprint pipeline instead. The epic plan content below (including its Refinements section) is preserved verbatim as the original requirement.

## Content

# Epic Plan 32.1: Multi-Tier Fix Execution Engine (Configurable Auto-Fix)

- **Estimated time**: TBD
- **Tasks/Components**: 5 tasks / 4 components
- **Execution**: init-plan

## Objective

Evolve the ATCR "Fixer" from a naive, single-model executor into a highly configurable, multi-tier execution engine. This allows users to route simple fixes to fast/cheap local models (like Ollama/Llama 3) while reserving expensive frontier models (like GPT-4o or Gemini 1.5) for complex architectural bugs.

## Context

Currently, the `atcr` executor acts as a single-model mop-up pass, blindly attempting to fix any finding that meets a minimum severity threshold (`min_severity_for_fix`) and has HIGH confidence. This means expensive models are wasted on trivial syntax errors, or conversely, cheap local models fail on complex logic bugs.
By introducing complexity estimates (via the Reviewer agents) and complexity ceilings (via the Executor configuration), we enable a Tiered Fix Pipeline that perfectly aligns with the BYO-Keys architecture, saving users money and neutralizing SaaS competitors like CodeRabbit and Gitar.ai that abstract this routing away.

## Proposed Solution

1. **Complexity Scoring in Review:** Update the core Reviewer agent prompts (`community_schema`) so that every finding includes an `est_minutes` or `complexity_score` metric.
2. **Multi-Tier Configuration:** Update `internal/registry/config.go` to support multiple executors in an ordered list, or add ceiling configuration options (e.g., `max_severity_for_fix`, `max_estimated_minutes`, `max_tool_calls`) to the existing `ExecutorConfig` so different models can be chained.
3. **Execution Routing Engine:** Refactor `internal/fanout/engine.go` (and `internal/verify/executor.go`) so `generateFixes` evaluates each finding against the executor's complexity bounds. If a finding exceeds Tier 1's bounds, it skips and passes it to Tier 2.
4. **Self-Gating (Confident Mode):** Allow the executor to self-assess and decline a fix if it realizes the change is too complex, ensuring it doesn't hallucinate a partial fix.

## Acceptance Criteria

- [ ] AC1: Reviewer agents output an estimated complexity/time metric for every finding.
- [ ] AC2: `atcr.yaml` configuration supports defining complexity ceilings for the executor (or defining multiple executors in a fallback chain).
- [ ] AC3: The Execution Engine successfully skips findings that exceed the configured complexity boundaries.
- [ ] AC4: A multi-tier workflow can be successfully run: a cheap model knocks out LOW complexity bugs, and a second run (or secondary tier) tackles the remaining HIGH complexity bugs.

## Tasks

- **T1**: Update the Reviewer prompt and finding schema in `internal/personas/community_schema.go` to require an `est_minutes` or complexity field.
- **T2**: Update the `ExecutorConfig` struct in `internal/registry/config.go` to support `max_severity_for_fix` and `max_estimated_minutes` (and validate these new fields).
- **T3**: Refactor `generateFixes` in `internal/fanout/engine.go` to filter findings based on the executor's upper bounds before attempting a fix.
- **T4**: Update `internal/verify/executor.go` to gracefully handle and log skipped findings when a complexity ceiling is hit.
- **T5**: Update `docs/registry.md` and user-facing documentation to explain the new multi-tier auto-fix capabilities and provide an example configuration.

## Components Touched

- `internal/personas/` (Prompts and Schema)
- `internal/registry/` (Configuration loading)
- `internal/fanout/` (Execution Loop routing)
- `internal/verify/` (Executor payload bounds)

## Refinements (2026-07-17)

This section records findings from `/refine-epic` run on July 17, 2026 05:10:00PM. It is additive — original plan content above is preserved.

### Auto-applied corrections (0)

None.

### Items needing user confirmation (2)

- ⏸️ **Stale path in T1:** File path `internal/personas/community_schema.go` cited in plan does not exist. The schema structure is defined in `internal/stream/parser.go`, the YAML catalog schema in `internal/registry/validate.go`, and the base reviewer prompt template in `personas/_base.md`.
- ⏸️ **Incorrect file location for `generateFixes` in T3:** The plan states `generateFixes` lives in `internal/fanout/engine.go` (and `internal/verify/executor.go`), but `generateFixes` actually lives only in `internal/verify/executor.go`.

### Advisory observations (1)

- ℹ️ **Scope-guard violation:** Derived TASK_COUNT=5, COMPONENT_COUNT=4 — exceeds the execute-epic skill's ≤6 tasks / ≤2 components limit. This plan will be rejected by the execute-epic skill and should run through `the init-plan skill @.planning/epics/active/48.0_multi_tier_fix_execution.md` for the full sprint pipeline. Refining alone will not unblock /execute-epic.

### Verification context

- Refinement depth: deep
- Derived TASK_COUNT: 5 (limit: 6)
- Derived COMPONENT_COUNT: 4 (limit: 2)
- COMPONENTS_TOUCHED: `internal/personas/`, `internal/registry/`, `internal/fanout/`, `internal/verify/`
- VISUAL_SURFACE: false
- HAS_GATED_WORK: false
- HAS_CROSS_SYSTEM: false
- Cited references checked: 7
- Codebase search queries (spot-check):
  - `validateExecutor registry validate ExecutorConfig`
  - `generateFixes invokeExecutor verify findings`
  - `reconcile findings EstMinutes severity`
  - `atcr config executor registry`
- Deep discovery method: semantic
- Deep discovery queries: ["validateExecutor registry validate ExecutorConfig", "generateFixes invokeExecutor verify findings", "reconcile findings EstMinutes severity", "atcr config executor registry"]
- Deep discovery match count: 10
- Deep discovery snapshot: `.planning/.temp/refine-epic/codebase-discovery.json` (temp-only — not committed)

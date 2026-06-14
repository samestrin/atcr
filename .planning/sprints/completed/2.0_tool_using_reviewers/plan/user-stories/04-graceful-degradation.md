# User Story 4: Graceful Degradation

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** reviewer operator running a mixed roster of tool-capable and non-tool-capable models
**I want** tool-enabled and non-tool agents to coexist in a single review, with any agent that cannot honor `tools: true` degrading transparently to single-shot and recording that fact in `status.json`
**So that** I never have to babysit the roster before a review, the engine never fails because a model lacks function-calling support, and I can see at a glance which agents ran as agents and which fell back to single-shot.

## Story Context

- **Background:** Epic 2.0 activates tool-using agents against a heterogeneous pool of providers and models. The registry already reserves `tools`, `max_turns`, and `tool_budget_bytes` on `AgentConfig` (from Epic 1.1), but does not declare per-provider function-calling capability — that declaration lands in this epic. Models in the wild vary: some support OpenAI-style function calling, some do not. An operator may configure a lane with `tools: true` and route it to a model that cannot comply. The engine must not error out; it must degrade.
- **Assumptions:**
  - Provider function-calling capability is declared in the model registry, not probed at runtime (v1).
  - The reconciler and findings contract already consume `raw/<agent>/*.json` uniformly; they do not need to know whether a result came from a tool loop or a single-shot call.
  - Fallback agents are already defined in the lane configuration and are invoked on primary-agent failure.
- **Constraints:**
  - No dynamic capability probing — registry is the sole source of truth for `supports_function_calling`.
  - Degradation is per-agent, not per-slot: a fallback that is itself non-tool-capable may also degrade.
  - `status.json` is the operator's single pane of glass for degradation signals.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | User Story 1 (Agent Loop Execution), User Story 2 (Budget Enforcement), registry `supports_function_calling` declaration |

## Success Criteria (SMART Format)

- **Specific:** A review with a mixed roster — at least one tool-enabled agent and at least one non-tool agent — completes end-to-end; a non-tool-capable model invoked with `tools: true` completes single-shot and emits `tools_degraded: true` in its `AgentStatus` entry in `status.json`.
- **Measurable:** 100% of degrade events surface `tools_degraded: true` on the agent's status entry; mixed-roster reviews produce a combined result consumed by reconcile with no reconcile-path errors attributable to tool-vs-single-shot heterogeneity; zero engine panics or hard failures from the degrade path across integration tests.
- **Achievable:** The degrade decision is a single branch in `invokeAgent` after the capability check against the registry; the existing 1.0 single-shot `Completer` path is reused unchanged.
- **Relevant:** Operators tune rosters for cost/quality, not for function-calling compatibility. Transparent degradation keeps the operator in control and preserves review throughput across model swaps.
- **Time-bound:** Implemented and covered by integration tests within the epic's sprint window; regression-tested in CI for every subsequent release.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-single-shot-degradation-path.md) | Single-Shot Degradation Path | Unit |
| [04-02](../acceptance-criteria/04-02-tool-capable-agent-loop-path.md) | Tool-Capable Agent Loop Path | Unit |
| [04-03](../acceptance-criteria/04-03-fallback-degradation-inheritance.md) | Fallback Degradation Inheritance | Unit |
| [04-04](../acceptance-criteria/04-04-mixed-roster-reconciler-compatibility.md) | Mixed Roster Reconciler Compatibility | Integration |

## Original Criteria Overview

1. A non-tool-capable model invoked with `tools: true` executes the single-shot path and its `AgentStatus` in `status.json` carries `tools_degraded: true` with the original `tools_requested: true` preserved.
2. A tool-capable model invoked with `tools: true` executes the multi-turn loop and its `AgentStatus` carries `tools_degraded: false` (field absent or false).
3. A single review may run a mixed roster — tool-loop agents and single-shot agents coexist — and the reconciler consumes both result shapes identically without special-casing.
4. Fallback agents inherit the effective `tools` setting of the lane invocation; when the fallback model is non-tool-capable, it too degrades independently and records its own `tools_degraded: true`.
5. Degradation is per-agent, not per-slot: the same lane can produce a tool-loop result on the primary and a degraded single-shot result on the fallback, both valid.
6. The degrade decision does not alter the findings contract, the reconcile input, or the report output — only `status.json` carries the signal.

## Technical Considerations

- **Implementation Notes:**
  - Add a `SupportsFunctionCalling bool` field (or equivalent) to the registry's model/provider descriptor, populated from `registry.yaml`. Default is `false`; opt-in per model entry. The documentation update for `supports_function_calling` and the active-fields table in `docs/registry.md` is owned by [User Story 6: Persona Guidance & Documentation](06-persona-guidance-documentation.md).
  - In `fanout/engine.go` `invokeAgent`, before the tool loop starts, consult the registry. If `Agent.Tools == true` and the resolved model lacks `SupportsFunctionCalling`, skip the loop, invoke the existing single-shot `Completer`, and set `AgentStatus.ToolsDegraded = true`.
  - `AgentStatus` gains `ToolsDegraded bool` and preserves the requested state (either via `ToolsRequested bool` or by leaving the `tools` field on the agent config visible in status).
  - Fallback invocation path already exists; thread the lane's resolved `tools` flag through to fallback dispatch. Each fallback agent re-evaluates degradation against its own model's capability — degrade is per-agent.
  - Reconciler and the rest of the downstream pipeline require no changes: they already read `raw/<agent>/*.json` and `status.json` without branching on tool-vs-single-shot. Verify this in tests.
- **Integration Points:**
  - `internal/fanout/engine.go` — degrade branch in `invokeAgent`, fallback threading.
  - `internal/fanout/status.go` (or equivalent) — new `ToolsDegraded` field on `AgentStatus`.
  - Registry loader — surface the `supports_function_calling` declaration from `registry.yaml`.
  - `status.json` schema — add the `tools_degraded` signal; remain backward-compatible (field absent when no degradation was evaluated).
  - Reconcile path — no code change expected; add a regression test with a mixed roster.
- **Data Requirements:**
  - `status.json` per-agent entry gains `tools_degraded: bool`.
  - Registry entries carry a `supports_function_calling: bool` per model (or per provider with per-model overrides).
  - No changes to `manifest.json` schema for this story beyond reflecting the resolved tools mode per agent.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Registry incorrectly declares a model's capability (false positive) — tool loop is attempted on a model that silently ignores `tools` | High | Default to `false`; require explicit opt-in per model. Add an integration test per declared tool-capable model that asserts `tool_calls` actually round-trip. Log a warning when the first response in a loop contains no `tool_calls` but the agent is configured for tools (hint, not a hard failure). |
| Fallback degradation is silent — operator never notices the fallback was not running as an agent | Medium | Always emit `tools_degraded: true` on degrade, including fallbacks. Aggregate a top-level `degraded_count` counter in `status.json` so mixed results are visible at a glance without drilling into per-agent entries. |
| Reconciler or a downstream consumer assumes uniform tool-vs-single-shot results and regresses | Low | Add a mixed-roster integration test that exercises both shapes in one review. Assert reconcile output is identical whether the agent ran as a tool-loop or single-shot for the same logical input. |
| Per-agent degrade semantics confuse operators used to per-slot expectations | Low | Document clearly in `docs/registry.md` and `docs/payload-modes.md` that degradation is per-agent and that fallbacks may themselves degrade. `status.json` per-agent entry is the authoritative signal. |

---

**Created:** June 13, 2026
**Status:** AC Defined - Ready for Implementation

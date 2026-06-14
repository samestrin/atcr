# User Story 2: Budget Enforcement

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** platform operator running tool-using reviewer agents
**I want** to set per-agent budgets for turns, cumulative tool-result bytes, and wall-clock time
**So that** a misbehaving or thrashing agent cannot consume unbounded API cost or block the review pipeline

## Story Context

- **Background:** Epic 2.0 turns single-shot pool reviewers into multi-turn agents that can call `read_file`, `grep`, and `list_files` in a loop. Without budgets, a small model that enters a thrash loop (repeatedly reading the same file, or chasing a phantom across the whole tree) could issue hundreds of tool calls before the review times out at the infrastructure level. Three independent budgets — `max_turns`, `tool_budget_bytes`, `timeout_secs` — give the operator cost control and deterministic halting.
- **Assumptions:**
  - Operators configure budgets via the existing `AgentConfig` registry fields (`MaxTurns`, `ToolBudgetBytes`, `TimeoutSecs`), which already parse and validate but are inert in 1.x.
  - The default `max_turns` of 10 (when `tools: true`) is sufficient for typical evidence-gathering loops (3-10 tool calls).
  - `MaxAgentTurns=1000` is a hard upper constant that no configuration can exceed.
  - When a budget trips, the agent is asked for a final answer (best-effort) and the review succeeds with partial-success semantics, matching the existing fanout convention.
- **Constraints:**
  - No new third-party dependencies — budgets are enforced with Go stdlib (`time`, `context`, counters).
  - Budget enforcement must not alter the wire format or provider interaction; it is purely an engine-layer concern.
  - Counters (turns, tool_calls, tool_bytes) must propagate to `status.json` regardless of whether the agent completed, degraded, or tripped a budget.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | M |
| **Dependencies** | Story 1 (Agent Loop Execution) — the loop must exist before budgets can be enforced on it |

## Success Criteria (SMART Format)

- **Specific:** Each tool-using agent run enforces `max_turns`, `tool_budget_bytes`, and `timeout_secs` as independent limits; when any limit trips, the engine requests a final answer from the model and records the tripped budget in `status.json`.
- **Measurable:** 100% of integration tests with scripted providers that exceed each budget individually (and combinations) terminate within the configured limit, produce a final answer (possibly partial), and emit the correct tripped-budget marker and counter values in `status.json`.
- **Achievable:** Budget enforcement is a set of counter checks inside the existing agent loop — no new infrastructure, no provider changes, no third-party dependencies.
- **Relevant:** Prevents runaway cost from tool-using agents, which is the primary economic risk identified in the plan's risk register ("Token cost explosion").
- **Time-bound:** Delivered within the Epic 2.0 sprint sequence; required before any adversarial-verification stories (Epic 3.0) can reuse the loop.

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [02-01](../acceptance-criteria/02-01-turn-budget-enforcement.md) | Turn Budget Enforcement | Unit/Integration |
| [02-02](../acceptance-criteria/02-02-tool-byte-budget-enforcement.md) | Tool Byte Budget Enforcement | Unit/Integration |
| [02-03](../acceptance-criteria/02-03-timeout-enforcement.md) | Timeout Enforcement | Unit/Integration |
| [02-04](../acceptance-criteria/02-04-budget-status-reporting-partial-success.md) | Budget Status Reporting and Partial Success | Unit/Integration |

## Original Criteria Overview

1. `max_turns` enforced: agent loop halts after the configured number of turns and requests a final answer; counter in `status.json` matches actual turns executed.
2. `tool_budget_bytes` enforced: cumulative bytes of tool results are tracked; when the cap trips mid-turn, the current turn completes, the engine requests a final answer, and the tripped marker is recorded.
3. `timeout_secs` enforced: the agent's context carries a deadline; when it expires, the loop halts, a final answer is requested (with whatever state is available), and the tripped marker is recorded.
4. Default `max_turns=10` applied automatically when `tools: true` and `MaxTurns` is unset; explicit values are honored up to `MaxAgentTurns=1000`.
5. Multiple budgets can trip in one run; `status.json` records all tripped budgets and the counters (`turns`, `tool_calls`, `tool_bytes`) reflect actual usage.
6. Partial-success semantics hold: a budget-tripped agent still produces a review result; reconcile consumes it identically to a fully-completed agent.

## Technical Considerations

- **Implementation Notes:**
  - Turn counter incremented at the top of each loop iteration; checked against `max_turns` before issuing the next `Chat` call. When at the limit, inject a system message requesting the final answer instead of passing tool definitions.
  - Byte budget tracked as a running `int64` sum of `len(toolResult.Content)` after each tool execution. Checked against `tool_budget_bytes` after each tool call; trip is deferred to end-of-turn to avoid partial tool-state.
  - Timeout enforced via `context.WithTimeout` on the agent's root context, derived from `timeout_secs`. The `Chat` call and tool execution share this context.
  - `MaxAgentTurns=1000` is a package-level constant; registry validation clamps any configured value above it to the constant.
  - Registry defaults applied in `AgentConfig.Resolve()` (or equivalent): when `Tools=true` and `MaxTurns==0`, set `MaxTurns=10`.
  - Registry activation and documentation updates (`docs/registry.md` active-field table, cost guidance) are owned by [User Story 6: Persona Guidance & Documentation](06-persona-guidance-documentation.md).
  - Tripped budgets recorded in `AgentStatus` as a slice of strings (e.g., `["max_turns", "tool_budget_bytes"]`) plus counters `Turns int`, `ToolCalls int`, `ToolBytes int64`.
- **Integration Points:**
  - `internal/fanout/engine.go` — `invokeAgent` loop reads budgets from `AgentConfig`, enforces them per-iteration.
  - `internal/fanout/result.go` (or equivalent) — `Result` and `AgentStatus` structs carry the new counter and tripped-budget fields.
  - `internal/registry/` — `AgentConfig` validation and default resolution.
  - `status.json` writer — consumes new fields from `AgentStatus`.
- **Data Requirements:**
  - `status.json` schema gains: `tripped_budgets: []string`, `turns: int`, `tool_calls: int`, `tool_bytes: int`.
  - Registry validation error messages reference the `MaxAgentTurns=1000` upper bound.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Operator sets `max_turns` far too low (e.g., 1), causing agents to never gather enough evidence | Medium | Document recommended range (5-20); default of 10 when `tools: true`; validation rejects values < 1 |
| Byte budget trips mid-tool-call, leaving partial tool state visible to the model | Low | Defer trip check to end-of-turn; current tool result is delivered in full, then loop halts |
| Timeout expires during a `Chat` call, leaving the agent in an ambiguous state | Medium | Context cancellation propagates to `Chat`; engine catches the error, records `timeout_secs` as tripped, and produces a partial result with whatever was gathered |
| Budget counters not propagated to `status.json` on the degrade path (non-tool-capable model fallback) | Medium | Counters are written unconditionally in the `Result` builder, regardless of completion path; test the degrade path explicitly |
| `MaxAgentTurns=1000` constant too low for legitimate deep-dive reviews | Low | Document as a hard safety rail; operators needing more should split the review, not raise the cap; revisit in future epics if field evidence demands it |

---

**Created:** June 13, 2026
**Status:** AC Generated - Ready for Implementation

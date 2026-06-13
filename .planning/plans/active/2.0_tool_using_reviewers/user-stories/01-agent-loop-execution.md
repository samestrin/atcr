# User Story 1: Agent Loop Execution

**Plan:** [2.0: Tool-Using Reviewers](../plan.md)

## User Story

**As a** reviewer agent with tools enabled
**I want** to make multiple tool calls across turns to explore the repository beyond the payload
**So that** I can verify suspicions, find callers, and cite real evidence in my findings instead of hallucinating context I cannot see

## Story Context

- **Background:** The current fanout engine invokes each reviewer as a single-shot chat completion — send prompt, get response, done. Reviewers can only reason about what is in the payload, which causes them to hallucinate context (phantom APIs, wrong line numbers, invented callers), miss whole bug classes (the caller that passes nil, the invariant broken two packages away), and small models suffer most because they have weaker recall and no way to look things up. This story transforms `InvokeDirect` from a single request/response into a bounded multi-turn agent loop.
- **Assumptions:**
  - The OpenAI-compatible function-calling wire format (`tools` array, `tool_calls` in response, `role:"tool"` messages) is the lowest common denominator across providers and litellm.
  - Tool definitions (read_file, grep, list_files) and the tool dispatcher/path jail are built first (or in parallel) — this story consumes them but does not own their internal implementation.
  - The `ChatCompleter` interface is introduced alongside the existing `Completer` interface; `llmclient.Client` implements both. The doctor package's separate `Completer` (internal/doctor/run.go:43) is unaffected.
  - Registry fields `Tools`, `MaxTurns`, `ToolBudgetBytes` are already parsed and validated in 1.x (internal/registry/config.go:54) — this story activates them, it does not design them.
- **Constraints:**
  - No new third-party dependencies — Go stdlib only for the tool harness.
  - The tool loop must respect the existing per-agent `context.WithTimeout` (engine.go:237) — timeout covers the whole loop, not a single turn.
  - Read-only enforcement is structural: no write tool exists, files opened read-only, no shell execution, no network access.
  - Fallback agents inherit the effective tools setting of the lane invocation; degrade is per-agent, not per-slot.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | High |
| **Effort Estimate** | L |
| **Dependencies** | Tool harness (definitions, dispatcher) — can be built in parallel; ChatCompleter interface in llmclient |

## Success Criteria (SMART Format)

- **Specific:** `InvokeDirect` runs as a multi-turn loop when `Agent.Tools` is true — it sends messages plus tool definitions, executes any returned `tool_calls` locally via the dispatcher, appends `role:"tool"` results to the message history, and repeats until the model returns a final assistant message or a budget trips.
- **Measurable:** A tool-enabled agent completes at least one multi-turn review against a fixture repo where it reads a file outside the payload, greps for callers, and produces findings that cite the evidence it actually read. Unit tests with scripted `httptest` mock providers cover: normal multi-turn completion, each budget trip (turns, bytes, timeout), loop hygiene (repeated tool call nudge, malformed JSON retry), and the degrade path.
- **Achievable:** The existing `Completer` interface and `invokeAgent` method (engine.go:228) provide a clean seam. Reserved registry fields and status counters already exist — activation is wiring, not redesign. The `httptest` pattern is established in `internal/llmclient/client_test.go`.
- **Relevant:** This is the core transformation of Epic 2.0 — without the agent loop, reviewers remain single-shot and cannot overcome the hallucination and evidence gaps that motivate the entire epic.
- **Time-bound:** Completed as the first story in Epic 2.0, unblocking all downstream stories (budget enforcement, path jail integration, transcript/accounting, persona updates).

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [01-01](../acceptance-criteria/01-01-chatcompleter-interface-wire-format.md) | ChatCompleter Interface and Wire Format | Unit |
| [01-02](../acceptance-criteria/01-02-multi-turn-agent-loop.md) | Multi-Turn Agent Loop Execution | Unit |
| [01-03](../acceptance-criteria/01-03-per-agent-budget-enforcement.md) | Per-Agent Budget Enforcement | Unit |
| [01-04](../acceptance-criteria/01-04-loop-hygiene.md) | Loop Hygiene - Repeated Calls and Malformed JSON | Unit |
| [01-05](../acceptance-criteria/01-05-degrade-path-fallback-inheritance.md) | Degrade Path and Fallback Inheritance | Unit |
| [01-06](../acceptance-criteria/01-06-result-accounting-compat.md) | Result Accounting and Backward Compatibility | Unit + Integration |

## Original Criteria Overview

1. `ChatCompleter` interface defined alongside `Completer`; `llmclient.Client` implements `Chat(ctx, inv, messages, tools)` with `tools` array in request and `tool_calls`/`role:tool` in response.
2. `invokeAgent` branches on `Agent.Tools` — when true, drives the multi-turn loop: send messages + tools, execute `tool_calls` via dispatcher, append `role:"tool"` results, repeat until final message or budget trip.
3. Three per-agent budgets enforced: `max_turns` (default 10 when tools=true and unset), `tool_budget_bytes` (cumulative tool-result bytes), and `timeout_secs` (whole loop via existing per-agent context).
4. Loop hygiene: identical repeated tool call injects a nudge message once then halts; malformed tool-call JSON returns a tool error (one retry) then proceeds to final answer; tool execution error returned to model as tool result, never fatal.
5. Non-tool-capable model with `tools: true` degrades to single-shot; `tools_degraded: true` recorded in `AgentStatus` and `status.json`.
6. `Result` struct gains `Turns int`, `ToolCalls int`, `ToolBytes int64` fields; populated during loop execution; propagated to `statusFor()` in artifacts.go:176.
7. Fallback agents inherit the effective tools setting of the lane invocation; a fallback may be a non-tool agent (degrade is per-agent).
8. All tests pass: unit tests for loop logic with scripted httptest mock providers; existing tests remain green (single-shot path unchanged when `Agent.Tools` is false).

## Technical Considerations

- **Implementation Notes:**
  - **ChatCompleter interface** (internal/fanout/engine.go): Add `ChatCompleter` with `Chat(ctx context.Context, inv *llmclient.Invocation, messages []Message, tools []ToolDef) (*ChatResponse, error)`. Engine checks via type assertion when `Agent.Tools` is true — avoids polluting the base `Completer` interface. The doctor package's `Completer` (internal/doctor/run.go:43) is unaffected.
  - **llmclient wire format** (internal/llmclient/client.go): Extend `chatRequest` (line 155) with `Tools []ToolDef` (omitempty — omitted for non-tool agents). Extend `chatResponse` choices with `Message.ToolCalls []ToolCall` and `ToolCallID`. Add `role:"tool"` message variant. Add `Chat` method alongside existing `Complete` (line 165).
  - **Agent struct** (internal/fanout/engine.go:24): Add `Tools bool`, `MaxTurns int`, `ToolBudgetBytes int64` fields. Populated by `buildAgent` (review.go:411) from `AgentConfig`.
  - **Result struct** (internal/fanout/engine.go:56): Add `Turns int`, `ToolCalls int`, `ToolBytes int64`. Populated incrementally during the loop; consumed by `statusFor()` (artifacts.go:176).
  - **AgentStatus** (internal/fanout/status.go:225): Add `ToolsDegraded bool` field with `json:"tools_degraded,omitempty"`. Existing reserved `*int ToolCalls` (line 241), `*int64 ToolBytes` (line 242), `*int Turns` (line 240) are populated during loop execution.
  - **Default application** (internal/registry/config.go:209): When `Tools==true` and `MaxTurns==nil`, set `MaxTurns=10`. Add `DefaultMaxTurns=10` constant in precedence.go (alongside existing `MaxAgentTurns=1000` at line 15).
  - **PayloadContext** (internal/payload/template.go:15): Add `ToolsEnabled bool` field. Set by `buildAgent` (review.go:411) from `AgentConfig.Tools` before calling `RenderPrompt`. Enables `{{if .ToolsEnabled}}` persona sections; the actual persona content updates and evidence-citation rule are owned by [User Story 6: Persona Guidance & Documentation](06-persona-guidance-documentation.md).
  - **Fallback inheritance** (internal/fanout/review.go:460): `buildFallbackAgent` propagates `Tools`/`MaxTurns`/`ToolBudgetBytes` from the primary's effective `AgentConfig` — the fallback's own config, not the primary agent's runtime values.
  - **Loop structure:** The tool loop is a `for` loop inside `invokeAgent` (or a new `invokeAgentLoop` helper). Each iteration: call `ChatCompleter.Chat`, check for `tool_calls` in response, execute via dispatcher, append results as `role:"tool"` messages, check budgets, repeat. Exit on: final message (no tool_calls), budget trip, context cancellation, or loop hygiene halt.

- **Integration Points:**
  - `internal/fanout/engine.go` — `invokeAgent` (line 228) is the insertion point for the tool loop.
  - `internal/llmclient/client.go` — `Chat` method added alongside `Complete` (line 165); wire format extended in `chatRequest`/`chatResponse`.
  - `internal/fanout/review.go` — `buildAgent` (line 411) propagates tool fields; `buildFallbackAgent` (line 460) inherits tools setting.
  - `internal/fanout/artifacts.go` — `statusFor()` (line 176) propagates `Result.Turns`/`ToolCalls`/`ToolBytes` to `AgentStatus`.
  - `internal/registry/config.go` — `applyDefaults` (line 209) applies `max_turns=10` when `tools=true`.
  - `internal/payload/template.go` — `PayloadContext` (line 15) gains `ToolsEnabled` field.
  - `internal/payload/personas_render_test.go` — sample context updated with `ToolsEnabled:false` so existing tests pass.
  - `internal/tools/dispatch.go` — tool dispatcher consumed by the loop (built in parallel; this story calls it).

- **Data Requirements:**
  - `Message` struct extended with `Role string`, `Content string`, `ToolCalls []ToolCall`, `ToolCallID string` (for `role:"tool"` messages).
  - `ToolCall` struct: `ID string`, `Type string`, `Function struct{ Name string, Arguments string }`.
  - `ToolDef` struct: OpenAI function-calling JSON Schema format (`name`, `description`, `parameters`).
  - `ChatResponse` struct: `Message` (with potential `ToolCalls`), `FinishReason string`.

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Provider variance in function-calling dialects causes tool_calls to be dropped or malformed | High | Use strict lowest-common-denominator wire format (OpenAI tools/tool_calls/role:tool); litellm normalizes most providers. Degrade path catches incapable models: `tools_degraded: true`. Scripted httptest tests cover both well-formed and malformed responses. |
| Token cost explosion from unbounded looping | High | Three hard budgets per agent (max_turns=10 default, tool_budget_bytes, timeout_secs). Counters propagated to status.json for visibility. Default budgets are conservative; per-agent opt-in via `tools: true`. |
| Small models thrash tools (repeat same call, ignore results, loop forever) | Medium | Loop hygiene: identical repeated tool call triggers a nudge message once, then halts and requests final answer. Malformed JSON gets one retry then final answer. Conservative default max_turns=10 caps worst case. |
| Context window overflow from accumulated tool results across turns | Medium | `tool_budget_bytes` budget caps cumulative tool-result bytes. Per-call byte caps in the tool dispatcher (built in parallel) limit individual results. Truncation markers indicate when results are cut. |
| Regression in single-shot path for non-tool agents | Medium | `invokeAgent` branches on `Agent.Tools` — existing single-shot code path is unchanged when `Tools` is false. All existing engine tests must remain green. `ChatCompleter` is opt-in via type assertion, never required. |
| Fallback agent tool inheritance is incorrect or inconsistent | Low | Fallback inherits from the lane's effective AgentConfig, not the primary's runtime state. Degrade is per-agent — a non-tool fallback degrades independently. Unit tests cover fallback-with-tools and fallback-without-tools scenarios. |

---

**Created:** June 13, 2026
**Status:** Accepted - 6 Criteria Defined

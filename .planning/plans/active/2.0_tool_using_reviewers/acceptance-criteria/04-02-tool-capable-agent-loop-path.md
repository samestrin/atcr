# Acceptance Criteria: Tool-Capable Agent Loop Path

**Related User Story:** [04: Graceful Degradation](../user-stories/04-graceful-degradation.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Tool Loop | Go tool loop in `invokeAgent` | Multi-turn OpenAI function-calling loop |
| Registry Field | `SupportsFunctionCalling bool` | Consulted before loop starts |
| Status Fields | `ToolsDegraded bool`, `Turns *int`, `ToolCalls *int` | Existing Epic 2.0 fields on `AgentStatus` |
| Completer | `AgentCompleter` interface (new) | Extends `Completer` with `CompleteWithTools()` for tool-loop calls |
| Test Framework | `go test` + table-driven | Fake agent completer asserts loop invocation |

## Related Files
- `internal/fanout/engine.go` - modify: add capability check before tool loop, branch to loop when capable
- `internal/fanout/status.go` - modify: ensure `tools_degraded` is `false` when loop runs successfully
- `internal/registry/config.go` - modify: `SupportsFunctionCalling` field parsed and validated
- `internal/fanout/engine_test.go` - modify: test that tool-capable model with `tools: true` runs the loop

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [Registry Configuration](../documentation/registry.md) — `supports_function_calling: true` is opt-in per model. Only models verified to support OpenAI-style function calling should be declared.
- [Agent Loop Design](../documentation/agent-loop.md) — The tool loop runs multi-turn until `end_turn`, budget exhaustion, or `max_turns`. The degrade decision happens before the loop starts, not mid-loop.

### Spec alignment notes

- **Capability check is pre-loop only** — once the loop starts, the engine does not re-check capability. If the model silently ignores tools mid-loop, a warning is logged but the loop continues to its normal termination conditions.
- **`tools_degraded: false` is the explicit signal** — when the tool loop runs, the status clearly shows no degradation occurred. The field may also be absent (omitempty) for 1.x backward compat.
- **No double-counting of turns** — the loop's turn counter (`AgentStatus.Turns`) is only populated when the loop actually runs; degraded single-shot agents leave it nil.

## Happy Path Scenarios

**Scenario 1: Tool-capable model with `tools: true` executes multi-turn loop**
- **Given** an agent configured with `tools: true`
- **And** the agent's model entry has `supports_function_calling: true` in `registry.yaml`
- **When** `invokeAgent` is called
- **Then** the engine enters the multi-turn tool loop
- **And** the `AgentStatus` has `tools_degraded: false` (or field absent)
- **And** the `AgentStatus.Turns` field records the number of turns executed
- **And** the `AgentStatus.ToolCalls` field records the number of tool calls made

**Scenario 2: `tools_degraded` is explicitly false after successful loop**
- **Given** an agent completed the tool loop successfully
- **When** `AgentStatus` is serialized to status.json
- **Then** `tools_degraded` is `false` (or absent via omitempty)
- **And** `tools_requested` is `true`
- **And** `turns` is a non-nil positive integer

**Scenario 3: Tool loop terminates normally (end_turn, budget, max_turns)**
- **Given** a tool-capable agent is running the loop
- **When** the loop reaches a termination condition (end_turn, budget exhaustion, or max_turns)
- **Then** the agent produces a final response
- **And** `tools_degraded` remains `false`
- **And** the loop's termination reason is recorded for observability

## Edge Cases

**Edge Case 1: Model returns no `tool_calls` in first response despite capability declaration**
- **Given** a model declared `supports_function_calling: true`
- **When** the first loop response contains no `tool_calls` (model chose not to use tools)
- **Then** the loop terminates naturally (the model ended its turn)
- **And** `tools_degraded` remains `false` (the model was capable, it chose not to)
- **And** a warning is logged suggesting possible misconfiguration

**Edge Case 2: Agent has `tools: true` but `max_turns` is unset**
- **Given** an agent with `tools: true` and no explicit `max_turns`
- **When** the engine starts the loop
- **Then** the default `max_turns` (10, per docs/registry.md) is applied
- **And** the loop runs with the default cap

**Edge Case 3: Agent has `tools: true` but `tool_budget_bytes` is 0 (unlimited)**
- **Given** an agent with `tools: true` and `tool_budget_bytes: 0`
- **When** the loop runs
- **Then** no cumulative byte budget is enforced on tool results
- **And** only `max_turns` and `end_turn` bound the loop

## Error Conditions

**Error Scenario 1: Tool loop encounters a transport error mid-loop**
- **Error message:** "agent %s: tool loop failed at turn %d: %w"
- **Behavior:** The agent status records `status: failed` and the error. `tools_degraded` remains `false` (the agent was running as an agent, it failed, it did not degrade).

**Error Scenario 2: Registry declares `supports_function_calling: true` but the provider returns a non-OpenAI-compatible response**
- **Error detection:** Parse failure on `tool_calls` in the response
- **Error message:** "agent %s: model %s: response parse error: expected tool_calls array, got %s"
- **Behavior:** The loop terminates with an error. The agent does not silently degrade to single-shot.

## Performance Requirements
- **Capability Check Latency:** < 1ms per agent (registry map lookup, no I/O)
- **No Overhead When Capable:** The degrade branch is a single `if` that passes through to the loop; no additional allocation or computation
- **Status Write:** `tools_degraded: false` adds a single JSON field; same serialization cost as any other bool

## Security Considerations
- **No Runtime Probing:** The engine trusts the registry declaration; it does not probe the provider for capabilities, preventing information leakage
- **Explicit Declaration Required:** `supports_function_calling: true` must be set per model; there is no provider-level wildcard that enables all models by default (prevents accidental tool-loop on untested models)
- **Validation at Load Time:** Invalid `supports_function_calling` values (non-boolean) are rejected during registry parsing

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- `AgentConfig` with `tools: true` and `supports_function_calling: true`
- Fake agent completer that simulates multi-turn tool-loop responses
- `AgentStatus` assertions for `tools_degraded`, `turns`, `tool_calls`
- Loop termination scenarios: end_turn, max_turns, budget exhaustion

**Mock/Stub Requirements:**
- Fake `AgentCompleter` implementing `CompleteWithTools(ctx, inv) (AgentResponse, error)` to simulate tool-loop responses
- Registry with `supports_function_calling: true` for the test model

**Test Cases:**
1. `TestInvokeAgent_ToolCapableRunsLoop` — verifies loop is entered and `tools_degraded` is false
2. `TestInvokeAgent_LoopRecordsTurnCount` — verifies `Turns` and `ToolCalls` are populated
3. `TestInvokeAgent_LoopEndTurnNoDegradation` — model returns end_turn without tools; no degrade
4. `TestInvokeAgent_LoopMaxTurnsTermination` — loop stops at max_turns; status correct
5. `TestInvokeAgent_LoopTransportError` — loop fails mid-way; `tools_degraded` still false

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/... ./internal/registry/...`)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Tool-capable model with `tools: true` enters the multi-turn tool loop
- [ ] `AgentStatus.tools_degraded` is `false` (or absent) when loop runs
- [ ] `AgentStatus.Turns` and `AgentStatus.ToolCalls` are populated after loop completion
- [ ] Loop termination (end_turn, max_turns, budget) produces correct status without degradation

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Warning log for no-tool_calls-in-first-response is present and documented
- [ ] Error messages name the agent and turn number for debugging

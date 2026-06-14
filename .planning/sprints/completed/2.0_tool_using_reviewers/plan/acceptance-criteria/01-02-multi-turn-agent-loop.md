# Acceptance Criteria: Multi-Turn Agent Loop Execution

**Related User Story:** [01: Agent Loop Execution](../user-stories/01-agent-loop-execution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Loop Driver | Go `for` loop in `invokeAgent` or `invokeAgentLoop` helper | `internal/fanout/engine.go` |
| Tool Dispatch | `internal/tools/dispatch.go` | Consumed, not owned by this AC |
| Test Framework | `go test` + `net/http/httptest` | Multi-turn scripted mock provider |
| Key Dependencies | `context`, `encoding/json` (stdlib only) | |

### Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:228` - modify: `invokeAgent` branches on `Agent.Tools` to drive multi-turn loop
- `internal/fanout/engine.go:24` - modify: extend `Agent` struct with `Tools`, `MaxTurns`, `ToolBudgetBytes`
- `internal/fanout/engine.go:56` - modify: extend `Result` struct with `Turns`, `ToolCalls`, `ToolBytes`
- `internal/fanout/engine_test.go` - create: multi-turn loop tests with scripted providers
- `internal/fanout/review.go:411` - modify: `buildAgent` propagates `Tools`/`MaxTurns`/`ToolBudgetBytes` to `Agent` struct
- `internal/tools/dispatch.go` - consume: tool dispatcher invoked by the loop (built in parallel)

## Happy Path Scenarios
**Scenario 1: Two-turn tool loop (model calls tool, then produces final answer)**
- **Given** an agent with `Tools: true` and a mock provider that returns a `tool_calls` response on the first turn, then a final assistant message on the second turn
- **When** `invokeAgent` executes
- **Then** the loop runs exactly 2 turns: first turn executes the tool call via dispatcher, second turn returns the final message; `Result.Turns == 2` and `Result.ToolCalls == 1`

**Scenario 2: Three-turn loop with multiple tool calls per turn**
- **Given** an agent with `Tools: true` and a mock provider that returns 2 `tool_calls` on turn 1, 1 `tool_call` on turn 2, and a final message on turn 3
- **When** `invokeAgent` executes
- **Then** the loop runs 3 turns; `Result.ToolCalls == 3`; tool results from each turn are appended as `role:"tool"` messages before the next turn

**Scenario 3: invokeAgent branches on Agent.Tools**
- **Given** an agent with `Tools: false`
- **When** `invokeAgent` is called
- **Then** the single-shot `Complete` path is used (unchanged from pre-story behavior); no tool loop entered

**Scenario 4: Agent struct populated from AgentConfig**
- **Given** an `AgentConfig` with `Tools: true`, `MaxTurns: 5`, `ToolBudgetBytes: 8192`
- **When** `buildAgent` constructs the `Agent`
- **Then** the resulting `Agent` has `Tools == true`, `MaxTurns == 5`, `ToolBudgetBytes == 8192`

## Edge Cases
**Edge Case 1: Model returns tool_calls with empty Function.Name**
- **Given** a mock provider returns a `tool_call` with `Function.Name: ""`
- **When** the dispatcher receives the call
- **Then** the tool error "unknown tool: " is returned as a `role:"tool"` result; the loop continues

**Edge Case 2: Context cancelled mid-loop**
- **Given** the agent's context is cancelled after turn 1 but before turn 2 starts
- **When** the loop checks context before the next `Chat` call
- **Then** the loop exits with a context error; partial results are captured in `Result`

**Edge Case 3: Dispatcher returns empty result string**
- **Given** a tool call executes successfully but returns an empty string
- **When** the result is appended as a `role:"tool"` message
- **Then** the message content is `""` (empty string); the loop continues normally

## Error Conditions
**Error Scenario 1: Tool execution panics**
- **Given** a tool in the dispatcher panics due to an internal bug
- **When** the panic is recovered by the dispatcher
- **Then** the error is returned as a `role:"tool"` result; the loop continues (tool errors are never fatal to the loop)

**Error Scenario 2: ChatCompleter.Chat returns error mid-loop**
- **Given** `Chat` succeeds on turn 1 but returns an error on turn 2
- **When** the error occurs
- **Then** the loop exits; the error is propagated to the caller; partial `Result` includes `Turns: 1`

## Performance Requirements
- **Response Time:** Each loop iteration adds no more than 2ms overhead for marshalling/dispatch beyond the `Chat` call latency
- **Throughput:** Loop is sequential per agent (not concurrent within a single agent's turns); concurrent agents are independent

## Security Considerations
- **Authentication/Authorization:** Tool execution uses existing file-system permissions; no new privilege surface
- **Input Validation:** Tool call `Function.Arguments` validated by dispatcher (JSON parse); invalid arguments return tool error, not loop crash

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Mock provider returning scripted multi-turn sequences: single tool call → final, multiple tools per turn, error responses, context cancellation
**Mock/Stub Requirements:** httptest mock provider; mock tool dispatcher (or real dispatcher with test fixtures); `Agent` struct with various `Tools`/`MaxTurns` configurations

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `invokeAgent` enters tool loop when `Agent.Tools == true`
- [ ] `invokeAgent` uses single-shot path when `Agent.Tools == false`
- [ ] Tool call results are appended as `role:"tool"` messages between turns
- [ ] `Result.Turns` and `Result.ToolCalls` are match the actual number of turns and tool calls executed

**Manual Review:**
- [ ] Code reviewed and approved

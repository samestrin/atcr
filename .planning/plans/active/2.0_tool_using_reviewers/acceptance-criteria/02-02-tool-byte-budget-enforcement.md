# Acceptance Criteria: Tool Byte Budget Enforcement

**Related User Story:** [02: Budget Enforcement](../user-stories/02-budget-enforcement.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Byte counter | Go `int64` running sum | Accumulates `len(toolResult.Content)` after each tool execution |
| Budget check location | End-of-turn in agent loop | Trip deferred to avoid partial tool-state |
| Budget source | `AgentConfig.ToolBudgetBytes *int64` | Already parsed in registry; 0 means unlimited |
| Tripped marker | String in `TrippedBudgets []string` on `AgentStatus` | e.g., `"tool_budget_bytes"` |
| Test framework | `go test` + fake `Completer` with tool results of known byte sizes |

## Related Files

- `internal/fanout/engine.go` — modify: add byte counter accumulation after each tool result; check against budget at end-of-turn
- `internal/fanout/engine.go` — modify: when budget trips, halt loop and request final answer
- `internal/fanout/status.go` — modify: `AgentStatus` struct gains `TrippedBudgets []string` and `ToolBytes *int64` fields (Turns already reserved)
- `internal/registry/config.go` — reference: `ToolBudgetBytes *int64` field already parsed and validated
- `internal/fanout/engine_test.go` — create: tests for byte budget enforcement

## Happy Path Scenarios

**Scenario 1: Byte budget trips after tool call exceeds cumulative limit**
- **Given** an agent configured with `ToolBudgetBytes=1000` and `Tools=true`
- **And** the agent's tool calls return results of 400, 400, and 500 bytes across 3 turns
- **When** the agent loop executes
- **Then** after turn 1: cumulative bytes = 400 (under budget)
- **And** after turn 2: cumulative bytes = 800 (under budget)
- **And** after turn 3: cumulative bytes = 1300 (over budget, trip detected at end-of-turn)
- **And** the current turn (turn 3) completes fully — all tool results delivered to the model
- **And** the loop halts; a system message requests a final answer
- **And** `AgentStatus.ToolBytes` = 1300
- **And** `AgentStatus.TrippedBudgets` contains `"tool_budget_bytes"`

**Scenario 2: Byte budget not tripped — agent completes under limit**
- **Given** an agent configured with `ToolBudgetBytes=5000` and `Tools=true`
- **And** the agent makes 3 tool calls returning 200, 300, 100 bytes then produces a final answer
- **When** the agent loop executes
- **Then** cumulative bytes = 600 (under 5000 budget)
- **And** the loop completes normally
- **And** `AgentStatus.ToolBytes` = 600
- **And** `AgentStatus.TrippedBudgets` does NOT contain `"tool_budget_bytes"`

**Scenario 3: Unlimited byte budget (ToolBudgetBytes=0 or nil)**
- **Given** an agent configured with `ToolBudgetBytes=0` (or nil/unset) and `Tools=true`
- **And** the agent makes many large tool calls
- **When** the agent loop executes
- **Then** no byte budget check is performed
- **And** the loop is not halted due to byte budget
- **And** `AgentStatus.TrippedBudgets` does NOT contain `"tool_budget_bytes"`

## Edge Cases

**Edge Case 1: Byte budget exactly met**
- **Given** an agent configured with `ToolBudgetBytes=1000`
- **And** tool calls return results totaling exactly 1000 bytes
- **When** the agent loop executes
- **Then** cumulative bytes = 1000 which equals the budget (not exceeded)
- **And** the budget is NOT tripped (trip condition is strictly greater than)
- **And** the loop continues to the next turn

**Edge Case 2: Single tool result exceeds entire byte budget**
- **Given** an agent configured with `ToolBudgetBytes=100`
- **And** a single tool call returns 500 bytes
- **When** the agent loop executes
- **Then** the tool result is delivered in full to the model (no partial delivery)
- **And** at end-of-turn, cumulative bytes = 500 exceeds budget of 100
- **And** the trip is recorded; loop halts and requests final answer

**Edge Case 3: Byte budget trips on the same turn as max_turns**
- **Given** an agent with `MaxTurns=3` and `ToolBudgetBytes=500`
- **And** on turn 3, tool results push cumulative bytes to 600
- **When** the end-of-turn checks run
- **Then** BOTH `max_turns` and `tool_budget_bytes` are recorded in `TrippedBudgets`
- **And** the loop halts; final answer is requested

**Edge Case 4: Tool returns empty result**
- **Given** an agent with `ToolBudgetBytes=1000`
- **And** a tool call returns an empty result (0 bytes)
- **When** the result is processed
- **Then** cumulative bytes remains unchanged (adds 0)
- **And** the loop continues normally

## Error Conditions

**Error Scenario 1: Tool call itself fails (not a budget issue)**
- **Given** an agent with `ToolBudgetBytes=1000`
- **And** a tool call returns an error (e.g., file not found)
- **When** the tool execution fails
- **Then** the error is delivered to the model as the tool result (per standard tool-use protocol)
- **And** no bytes are added to the counter (error result has no content bytes)
- **And** the budget is NOT tripped from this error alone

**Error Scenario 2: Context timeout occurs while tracking byte budget**
- **Given** an agent with `ToolBudgetBytes=5000` and `TimeoutSecs=30`
- **And** cumulative bytes = 2000 after 2 turns
- **And** the context deadline expires during turn 3
- **When** the timeout fires
- **Then** the timeout takes precedence over byte budget
- **And** `AgentStatus.ToolBytes` = 2000 (whatever was accumulated)
- **And** `AgentStatus.TrippedBudgets` contains `"timeout_secs"` but NOT `"tool_budget_bytes"`

## Performance Requirements

- **Byte accumulation overhead:** `len()` on a string is O(1) in Go; addition to an int64 is negligible
- **No additional allocations:** The byte counter is a single int64 variable per agent invocation
- **Check frequency:** Budget check occurs once per turn (at end-of-turn), not per tool call within a turn — minimizes branching overhead

## Security Considerations

- **No memory amplification:** The byte budget limits cumulative tool-result data the model sees, preventing a tool loop from feeding unbounded data into the LLM context window
- **Deferred trip prevents partial state:** By checking at end-of-turn rather than mid-tool-call, the model never sees a truncated tool result, which could cause confusion or hallucination
- **Validation at load time:** `ToolBudgetBytes < 0` is rejected at registry load (`"tool_budget_bytes must be >= 0"`), preventing negative budget misconfiguration

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Fake `Completer` returning tool-call responses with known content sizes (e.g., `strings.Repeat("x", 400)`)
- `AgentConfig` fixtures with `ToolBudgetBytes`: nil, 0, 100, 1000, very large value
- Scripted sequences: under-budget, exactly-on-budget, over-budget, single-oversize-tool

**Mock/Stub Requirements:**
- Fake `Completer` that returns tool calls with controllable result sizes on each turn
- The fake must track cumulative bytes returned to verify against `AgentStatus.ToolBytes`

**Test Cases to Implement:**
1. Byte budget trips when cumulative tool bytes exceed limit
2. Byte budget not tripped when agent stays under limit
3. Unlimited budget (0/nil) never trips
4. Exactly-on-budget (cumulative == limit) does NOT trip
5. Single oversized tool result completes fully before trip
6. Byte budget + turn budget both trip on same turn
7. `AgentStatus.ToolBytes` accurately reflects cumulative bytes
8. `AgentStatus.TrippedBudgets` contains `"tool_budget_bytes"` only when tripped

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Cumulative tool-result bytes tracked accurately as int64 running sum
- [ ] Budget trip detected at end-of-turn; current turn completes fully before halting
- [ ] `AgentStatus.ToolBytes` records actual cumulative bytes in `status.json`
- [ ] `AgentStatus.TrippedBudgets` contains `"tool_budget_bytes"` when and only when budget is exceeded
- [ ] `ToolBudgetBytes=0` or nil means unlimited — no trip occurs

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Deferred-trip behavior verified: tool result delivered in full even when it alone exceeds budget
- [ ] Byte counter correctness verified against manual calculation in test output

# Acceptance Criteria: Per-Agent Budget Enforcement

**Related User Story:** [01: Agent Loop Execution](../user-stories/01-agent-loop-execution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Budget Counters | Go struct fields (`Turns int`, `ToolBytes int64`) | Accumulated in loop |
| Default Application | `registry/config.go` `applyDefaults` | `MaxTurns=10` when `Tools==true` and unset |
| Timeout | Existing `context.WithTimeout` on per-agent basis | `engine.go:237` |
| Test Framework | `go test` + `net/http/httptest` | Scripted providers that trip budgets |
| Key Dependencies | `context`, `time` (stdlib) | |

### Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:228` - modify: budget checks inside the tool loop (turns, bytes, timeout)
- `internal/registry/config.go:209` - modify: `applyDefaults` sets `MaxTurns=10` when `Tools==true` and `MaxTurns` unset
- `internal/registry/precedence.go:15` - modify: add `DefaultMaxTurns = 10` constant alongside `MaxAgentTurns=1000`
- `internal/fanout/engine_test.go` - create: tests for each budget trip (turns exceeded, bytes exceeded, timeout)

## Happy Path Scenarios
**Scenario 1: max_turns budget trips**
- **Given** an agent with `MaxTurns: 3` and a mock provider that returns `tool_calls` on every turn
- **When** the loop reaches turn 3 and the model returns another `tool_calls`
- **Then** the loop halts before executing the turn-4 tool calls; a budget-trip message is injected as the final assistant content; `Result.Turns == 3`

**Scenario 2: tool_budget_bytes budget trips**
- **Given** an agent with `ToolBudgetBytes: 1000` and tool calls that return cumulative results exceeding 1000 bytes
- **When** the cumulative `ToolBytes` counter exceeds `ToolBudgetBytes` after a tool execution
- **Then** the loop halts; a budget-trip message is appended; `Result.ToolBytes >= 1000`

**Scenario 3: timeout enforced via existing per-agent context**
- **Given** an agent with a `context.WithTimeout` of 2 seconds and a mock provider that sleeps 3 seconds per turn
- **When** the first `Chat` call exceeds the context deadline
- **Then** the loop exits with a context deadline exceeded error; `Result.Turns` reflects completed turns only

**Scenario 4: Default MaxTurns applied when tools=true and MaxTurns unset**
- **Given** an `AgentConfig` with `Tools: true` and `MaxTurns: nil` (or 0)
- **When** `applyDefaults` runs
- **Then** `MaxTurns` is set to 10 (`DefaultMaxTurns`)

## Edge Cases
**Edge Case 1: Exact budget boundary (turns == MaxTurns with no tool_calls)**
- **Given** an agent with `MaxTurns: 5` and the model returns a final message (no `tool_calls`) on exactly turn 5
- **When** the loop processes the response
- **Then** the loop exits normally with the final message (budget not tripped — model finished within budget)

**Edge Case 2: tool_budget_bytes is 0 (unlimited or unset)**
- **Given** an agent with `ToolBudgetBytes: 0` (not configured)
- **When** the loop runs
- **Then** no byte budget check is enforced; only `max_turns` and `timeout` apply

**Edge Case 3: MaxTurns explicitly set to 1**
- **Given** an agent with `Tools: true` and `MaxTurns: 1`
- **When** the loop runs and the model returns `tool_calls` on turn 1
- **Then** the loop halts after turn 1; tool calls from turn 1 are NOT executed; budget-trip message injected

## Error Conditions
**Error Scenario 1: Context has no deadline (timeout_secs not set)**
- **Given** an agent context without a timeout (no `WithTimeout` applied)
- **When** the loop runs
- **Then** the `max_turns` budget still applies as the primary safety net; loop cannot run forever

**Error Scenario 2: Negative MaxTurns after config parsing**
- **Given** a configuration with `MaxTurns: -1`
- **When** `applyDefaults` runs
- **Then** the negative value is rejected during config validation (pre-story validation in registry package)

## Performance Requirements
- **Response Time:** Budget counter checks add O(1) overhead per iteration (integer comparison, no allocation)
- **Throughput:** Budget counters are per-agent (local to `invokeAgent`); no cross-agent synchronization needed

## Security Considerations
- **Authentication/Authorization:** N/A — budgets are resource limits, not access controls
- **Input Validation:** `ToolBudgetBytes` from config validated as non-negative; `MaxTurns` validated as positive integer

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Mock providers that: (a) always return tool_calls to force max_turns trip, (b) return large tool results to force byte budget trip, (c) sleep to force timeout, (d) config fixtures with nil/zero/explicit MaxTurns
**Mock/Stub Requirements:** httptest mock provider; tool dispatcher returning configurable-size results; `AgentConfig` fixtures

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/... ./internal/registry/...`)
- [ ] No linting errors (`go vet`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `max_turns` budget trips halt the loop and inject a budget message
- [ ] `tool_budget_bytes` budget trips halt the loop when cumulative bytes exceed limit
- [ ] Context timeout halts the loop mid-execution
- [ ] `DefaultMaxTurns=10` constant exists in `precedence.go` and is applied when Tools==true and MaxTurns is unset
- [ ] `Result.Turns`, `Result.ToolBytes` reflect actual budget consumption

**Manual Review:**
- [ ] Code reviewed and approved

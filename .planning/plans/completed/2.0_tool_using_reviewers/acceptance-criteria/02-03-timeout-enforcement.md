# Acceptance Criteria: Timeout Enforcement

**Related User Story:** [02: Budget Enforcement](../user-stories/02-budget-enforcement.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Timeout mechanism | `context.WithTimeout` | Derived from `AgentConfig.TimeoutSecs` or resolved `Settings.TimeoutSecs` |
| Context propagation | Go `context.Context` | Shared between `Chat` calls and tool execution within the agent loop |
| Timeout detection | `errors.Is(err, context.DeadlineExceeded)` | Already handled by `classifyStatus` in `engine.go` |
| Final answer on timeout | Best-effort with available state | Whatever turns/results gathered before timeout are preserved |
| Test framework | `go test` + `context.WithTimeout` or fake timer | Scripted provider with configurable delay |

### Related Files (from codebase-discovery.json)

- `internal/fanout/engine.go:237` — modify: apply per-agent `context.WithTimeout` to the agent loop root context
- `internal/fanout/engine.go:228` — modify: on context timeout within agent loop, halt loop, record tripped budget, produce partial result
- `internal/fanout/status.go` — reference: `classifyStatus` already maps `context.DeadlineExceeded` to `StatusTimeout`
- `internal/fanout/status.go:225` — modify: `AgentStatus` gains `TrippedBudgets []string` to record `"timeout_secs"`
- `internal/registry/config.go:54` — reference: `AgentConfig.TimeoutSecs *int` already parsed and validated
- `internal/registry/precedence.go` — reference: `MaxTimeoutSecs = 86400` constant
- `internal/fanout/engine_test.go` — create: tests for timeout enforcement in agent loop

## Happy Path Scenarios

**Scenario 1: Agent completes within timeout**
- **Given** an agent configured with `TimeoutSecs=30` and `Tools=true`
- **And** the agent completes its tool loop (3 turns + final answer) in 10 seconds
- **When** the agent loop executes
- **Then** the result status is `StatusOK`
- **And** `AgentStatus.TrippedBudgets` does NOT contain `"timeout_secs"`
- **And** the result contains the agent's full output

**Scenario 2: Agent timeout during a Chat call**
- **Given** an agent configured with `TimeoutSecs=5` and `Tools=true`
- **And** the agent is on turn 3 when the 5-second deadline expires
- **And** the scripted provider delays its response beyond 5 seconds
- **When** the context deadline fires mid-`Complete` call
- **Then** the `Complete` call returns a `context.DeadlineExceeded` error
- **And** the agent loop halts
- **And** `AgentStatus.Status` is `"timeout"`
- **And** `AgentStatus.TrippedBudgets` contains `"timeout_secs"`
- **And** `AgentStatus.Turns` records the number of turns completed before timeout (2 completed, or 3 if the partial turn is counted)
- **And** the partial result (with whatever was gathered in turns 1-2) is preserved and passed downstream

**Scenario 3: Agent timeout between turns (during tool execution)**
- **Given** an agent configured with `TimeoutSecs=5` and `Tools=true`
- **And** the agent is executing tool calls on turn 4 when the deadline expires
- **When** the context deadline fires during tool execution
- **Then** tool execution is interrupted by context cancellation
- **And** the agent loop halts
- **And** `AgentStatus.TrippedBudgets` contains `"timeout_secs"`
- **And** the partial result with turns 1-3 completed is preserved

**Scenario 4: Timeout uses resolved settings when agent-level unset**
- **Given** an agent with `TimeoutSecs=nil` (unset) and `Tools=true`
- **And** the resolved shared `Settings.TimeoutSecs=600`
- **When** the agent loop starts
- **Then** the effective timeout is 600 seconds (from shared settings)
- **And** the agent's context carries a 600-second deadline

## Edge Cases

**Edge Case 1: Timeout of 1 second**
- **Given** an agent configured with `TimeoutSecs=1` and `Tools=true`
- **And** the scripted provider takes 2 seconds per `Complete` call
- **When** the agent loop starts
- **Then** the first `Complete` call times out after 1 second
- **And** `AgentStatus.Turns` = 0 (no turns completed)
- **And** `AgentStatus.TrippedBudgets` contains `"timeout_secs"`
- **And** a partial/empty result is produced with status `"timeout"`

**Edge Case 2: Timeout expires at the exact moment a turn completes**
- **Given** an agent with `TimeoutSecs=10` and `Tools=true`
- **And** turn 5 completes at exactly the 10-second mark
- **When** the loop checks whether to continue
- **Then** the completed turn's results are preserved
- **And** the next `Complete` call either immediately times out or the context is already done
- **And** `AgentStatus.Turns` = 5

**Edge Case 3: Parent context cancelled before agent timeout**
- **Given** an agent with `TimeoutSecs=30` (per-agent timeout)
- **And** the parent/global context is cancelled at 5 seconds
- **When** the parent context cancellation propagates
- **Then** the agent loop halts (parent context is done)
- **And** the timeout is classified via `classifyStatus` as `StatusTimeout`
- **And** `AgentStatus.TrippedBudgets` contains `"timeout_secs"` (the effective cause)
- **And** sibling agents are not affected (their own contexts remain valid)

**Edge Case 4: Timeout=0 (no per-agent timeout)**
- **Given** an agent configured with `TimeoutSecs=0` or `nil` and no shared timeout
- **And** `Tools=true`
- **When** the agent loop executes
- **Then** no per-agent `context.WithTimeout` is applied
- **And** the agent is bounded only by the global context deadline (if any)
- **And** timeout budget enforcement is not active for this agent

## Error Conditions

**Error Scenario 1: Provider error coincides with timeout**
- **Given** an agent with `TimeoutSecs=10`
- **And** the provider returns a transport error at exactly the 10-second mark
- **When** the error occurs
- **Then** `classifyStatus` checks the error: if `context.DeadlineExceeded` is in the wrap chain, status is `"timeout"`
- **And** `AgentStatus.TrippedBudgets` contains `"timeout_secs"`
- **And** if the error is a non-context error, status is `"failed"` and timeout is NOT in tripped budgets

**Error Scenario 2: Context cancellation (not deadline) during loop**
- **Given** an agent with `TimeoutSecs=30`
- **And** the user sends SIGINT, cancelling the root context
- **When** `context.Canceled` propagates to the agent loop
- **Then** `classifyStatus` maps `context.Canceled` to `StatusTimeout` (existing behavior)
- **And** partial results are preserved
- **And** `AgentStatus.TrippedBudgets` records `"timeout_secs"` (context cancellation is treated equivalently to deadline per existing design)

## Performance Requirements

- **Timeout creation:** `context.WithTimeout` is O(1) and creates one timer per agent invocation; negligible overhead
- **Context check per iteration:** `ctx.Err()` is a simple nil check; negligible cost
- **No busy-waiting:** Timeout relies on Go's context cancellation mechanism (timer-based), not polling
- **Cleanup:** `cancel()` is always deferred after `context.WithTimeout`, preventing timer leaks

## Security Considerations

- **No unbounded execution:** Every tool-using agent has a timeout either from agent config, shared settings, or global review timeout — defense in depth
- **Context propagation to tools:** The same context carrying the deadline is passed to tool execution, so tools cannot bypass the timeout by blocking indefinitely
- **Validation at load time:** `TimeoutSecs <= 0` and `TimeoutSecs > 86400` are rejected at registry validation, preventing misconfiguration
- **No silent hangs:** Even if a provider never responds, the timeout ensures the loop terminates

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Fake `Completer` with configurable delay per call (e.g., `time.Sleep` or channel-based blocking)
- `AgentConfig` fixtures with `TimeoutSecs`: nil, 0, 1, 5, 30
- Scripted provider that returns tool calls on turns 1-N, then delays on turn N+1 to trigger timeout

**Mock/Stub Requirements:**
- Fake `Completer` that can be configured to delay its response by a specified duration
- The fake must be cancellable via context — when context is done, it should return `context.DeadlineExceeded`
- Optionally, a fake clock or `time.Now` override to avoid real delays in tests (preferred for fast CI)

**Test Cases to Implement:**
1. Agent completes within timeout — no budget trip
2. Provider delay causes timeout mid-`Complete` call — partial result with timeout status
3. Tool execution delay causes timeout — partial result preserved
4. Parent context cancellation halts agent loop — partial result with timeout status
5. `TimeoutSecs=nil` — no per-agent timeout applied (only global deadline)
6. Timeout of 1 second with slow provider — immediate timeout, turns=0
7. Verify `AgentStatus.TrippedBudgets` contains `"timeout_secs"` on timeout
8. Verify partial results (turns, tool_calls, tool_bytes) are recorded even on timeout

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Agent loop uses `context.WithTimeout` derived from effective timeout (agent-level or shared settings)
- [ ] Context deadline propagates to both `Chat` calls and tool execution
- [ ] On timeout, loop halts and partial result is preserved (not discarded)
- [ ] `AgentStatus.TrippedBudgets` contains `"timeout_secs"` when deadline expires
- [ ] `AgentStatus.Status` is `"timeout"` via existing `classifyStatus` logic
- [ ] Counters (`Turns`, `ToolCalls`, `ToolBytes`) reflect actual usage at time of timeout

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Context cancellation path verified: SIGINT during tool-using loop produces valid partial result
- [ ] Timeout behavior verified with a live scripted provider test (real delays, short timeout)

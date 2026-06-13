# Acceptance Criteria: Turn Budget Enforcement

**Related User Story:** [02: Budget Enforcement](../user-stories/02-budget-enforcement.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Turn counter | Go int variable in agent loop | Incremented at the top of each loop iteration |
| Default resolution | Go logic in registry or engine | When `Tools=true` and `MaxTurns==nil`, set to 10 |
| Hard cap constant | `MaxAgentTurns = 1000` in `registry/precedence.go` | Already defined; no configuration can exceed this |
| Final answer injection | System message appended to conversation | Replaces tool definitions when at limit |
| Test framework | `go test` + fake `Completer` | Scripted provider returns tool calls for N turns |

## Related Files

- `internal/fanout/engine.go` — modify: add turn counter and enforcement inside the agent loop (currently single-shot `invokeAgent`)
- `internal/fanout/engine.go` — modify: inject system message requesting final answer when turn limit reached
- `internal/registry/config.go` — modify: add default resolution for `MaxTurns` when `Tools=true` and `MaxTurns==nil`
- `internal/registry/precedence.go` — reference: `MaxAgentTurns = 1000` constant (already exists)
- `internal/fanout/status.go` — reference: `AgentStatus.Turns *int` field (already reserved)
- `internal/fanout/engine_test.go` — create: tests for turn budget enforcement

## Happy Path Scenarios

**Scenario 1: Agent halts at configured max_turns**
- **Given** an agent configured with `MaxTurns=5` and `Tools=true`
- **And** the agent's scripted provider returns a tool call on each turn
- **When** the agent loop executes
- **Then** the loop runs exactly 5 turns (turn counter = 5)
- **And** on the 6th iteration, tool definitions are withheld and a system message requests a final answer
- **And** the agent returns a final answer (possibly partial)
- **And** `AgentStatus.Turns` is set to 5 in `status.json`

**Scenario 2: Agent completes within budget**
- **Given** an agent configured with `MaxTurns=10` and `Tools=true`
- **And** the agent's scripted provider returns a final answer (no tool calls) on turn 3
- **When** the agent loop executes
- **Then** the loop completes after 3 turns
- **And** `AgentStatus.Turns` is set to 3 in `status.json`
- **And** no budget-tripped marker is recorded

**Scenario 3: Default max_turns=10 applied when tools enabled**
- **Given** an agent configured with `Tools=true` and `MaxTurns` unset (nil)
- **When** the agent configuration is resolved
- **Then** `MaxTurns` is set to 10 automatically
- **And** the agent loop enforces the 10-turn limit

**Scenario 4: Explicit MaxTurns honored up to MaxAgentTurns**
- **Given** an agent configured with `Tools=true` and `MaxTurns=500`
- **When** the registry is loaded
- **Then** `MaxTurns=500` is accepted and honored (500 <= 1000)
- **And** the agent loop enforces the 500-turn limit

## Edge Cases

**Edge Case 1: max_turns=1**
- **Given** an agent configured with `MaxTurns=1` and `Tools=true`
- **When** the agent loop executes
- **Then** exactly 1 turn is executed (the initial call with tools available)
- **And** the loop immediately requests a final answer without further tool calls
- **And** the result is valid (partial-success semantics apply)

**Edge Case 2: max_turns at upper boundary (1000)**
- **Given** an agent configured with `MaxTurns=1000` (equal to `MaxAgentTurns`)
- **When** the registry is loaded
- **Then** validation passes (1000 is within 1..1000)

**Edge Case 3: max_turns exceeding MaxAgentTurns**
- **Given** an agent configured with `MaxTurns=1001`
- **When** the registry is loaded
- **Then** validation fails with error message referencing `MaxAgentTurns` bound: `"agent '<name>': max_turns must be within 1..1000"`

**Edge Case 4: max_turns=0 or negative**
- **Given** an agent configured with `MaxTurns=0` or `MaxTurns=-1`
- **When** the registry is loaded
- **Then** validation fails with error: `"agent '<name>': max_turns must be within 1..1000"`

**Edge Case 5: Tools=false with MaxTurns set**
- **Given** an agent configured with `Tools=false` and `MaxTurns=5`
- **When** the agent is invoked
- **Then** the agent runs as a single-shot call (no loop); `MaxTurns` is ignored

## Error Conditions

**Error Scenario 1: Provider error during turn within budget**
- **Given** an agent with `MaxTurns=10` and the provider returns an error on turn 3
- **When** the agent loop executes
- **Then** the error is handled per existing fallback logic (try fallback agents or fail the slot)
- **And** `AgentStatus.Turns` records 3 (the turns actually attempted)
- **And** the tripped-budget marker is NOT set (this is a provider error, not a budget trip)

**Error Scenario 2: Context cancelled mid-loop**
- **Given** an agent with `MaxTurns=10` running in a loop
- **And** the parent context is cancelled (e.g., user interrupt)
- **When** the context cancellation propagates
- **Then** the loop halts immediately
- **And** `AgentStatus.Turns` records the turns completed before cancellation
- **And** the status is classified as `timeout` per existing `classifyStatus` logic

## Performance Requirements

- **Turn check overhead:** Turn counter check is an integer comparison per loop iteration; overhead is negligible (< 1 microsecond)
- **No provider round-trips for enforcement:** Budget checks happen locally in the engine; no additional network calls
- **Memory:** Turn counter is a single int variable per agent invocation; no additional allocations

## Security Considerations

- **Hard cap enforcement:** `MaxAgentTurns=1000` is a package-level constant that cannot be overridden by user configuration; validation rejects values above it
- **No operator bypass:** Even with `tools: true`, the agent cannot exceed `MaxAgentTurns` regardless of configuration
- **Input validation:** `MaxTurns <= 0` is rejected at load time, preventing zero-turn or negative-turn misconfiguration

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Fake `Completer` implementation that returns scripted tool calls for N consecutive turns, then a final answer
- `AgentConfig` fixtures with various `MaxTurns` values: nil, 1, 5, 10, 1000, 1001, 0, -1
- `Slot` fixtures with `Tools=true` agents

**Mock/Stub Requirements:**
- Fake `Completer` that records each `Complete` call and returns predefined tool-call responses for the first N calls, then returns a plain text response (final answer)
- The fake should track: number of `Complete` calls made, whether tool definitions were included in each call

**Test Cases to Implement:**
1. Agent halts at exactly `MaxTurns` and produces a final answer
2. Agent completes before reaching `MaxTurns` (no budget trip)
3. Default `MaxTurns=10` applied when `Tools=true` and `MaxTurns=nil`
4. Validation rejects `MaxTurns > 1000`, `MaxTurns <= 0`
5. `MaxTurns=1` results in exactly 1 turn with tools, then final answer request
6. Turn counter correctly recorded in `AgentStatus.Turns` for all scenarios
7. When at turn limit, tool definitions are omitted from the final `Complete` call

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/fanout/...`)
- [ ] All registry validation tests pass (`go test ./internal/registry/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Agent loop halts at configured `MaxTurns` and requests a final answer via system message
- [ ] Default `MaxTurns=10` is applied when `Tools=true` and `MaxTurns` is unset
- [ ] `MaxAgentTurns=1000` hard cap is enforced at validation time
- [ ] `AgentStatus.Turns` accurately reflects the number of turns executed in `status.json`

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Turn limit behavior verified with a live scripted provider test
- [ ] System message for final answer injection reviewed for clarity and model-compatibility

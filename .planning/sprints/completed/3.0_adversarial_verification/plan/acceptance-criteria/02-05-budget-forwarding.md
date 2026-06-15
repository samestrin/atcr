# Acceptance Criteria: Per-Finding Budget Forwarding

**Related User Story:** [[02]: Skeptic Invocation & Verdict Parsing](../user-stories/02-skeptic-invocation-verdict-parsing.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Budget Forwarding | Go package `internal/verify` | `invokeSkeptic` in `invoke.go` constructs `Agent` with budget fields |
| Tool Loop Budgets | `internal/fanout` | `invokeToolLoop` (loop.go:81) enforces MaxTurns, ToolBudgetBytes, timeout |
| Test Framework | `go test` + `testify` | Assert tripped budgets in Result and Verification |
| Key Dependencies | `internal/fanout` (Agent, Result.TrippedBudgets), `internal/registry` (AgentConfig) | Budget values flow from AgentConfig to Agent |

### Related Files (from codebase-discovery.json)

Files identified from codebase-discovery.json (line numbers refer to the discovery snapshot):

- `internal/fanout/loop.go:81` - reference: `invokeToolLoop` enforces budgets
- `internal/registry/config.go:56` - reference: `AgentConfig` struct (MaxTurns, ToolBudgetBytes, TimeoutSecs)

- `internal/verify/invoke.go` - modify: `invokeSkeptic` forwards `MaxTurns`, `ToolBudgetBytes`, `TimeoutSecs` from `AgentConfig` to `Agent`
- `internal/fanout/engine.go` - reference: `Agent` struct fields `MaxTurns` (line 64), `ToolBudgetBytes` (line 65), `TimeoutSecs` (line 56)
- `internal/fanout/loop.go` - reference: budget enforcement in `toolLoop.run` (line 106) — MaxTurns check (line 153), context deadline
- `internal/registry/config.go` - reference: `AgentConfig.MaxTurns` (line 68), `ToolBudgetBytes` (line 69), `TimeoutSecs` (line 61)

## Happy Path Scenarios
**Scenario 1: MaxTurns forwarded and enforced**
- **Given** a skeptic `AgentConfig` with `MaxTurns: 3` and a `ChatCompleter` that returns tool_calls on every turn
- **When** `invokeSkeptic` is called
- **Then** the tool loop halts after 3 turns, `Result.TrippedBudgets` contains `"max_turns"`, and the returned `Verification` has `Verdict: "unverifiable"` with `Notes` mentioning the tripped budget

**Scenario 2: ToolBudgetBytes forwarded and enforced**
- **Given** a skeptic `AgentConfig` with `ToolBudgetBytes: 100` and a `toolDispatcher` that returns 200-byte tool results
- **When** `invokeSkeptic` is called
- **Then** the tool loop halts when the byte budget is exhausted, `Result.TrippedBudgets` contains `"tool_budget_bytes"`, and the returned `Verification` has `Verdict: "unverifiable"`

**Scenario 3: Timeout forwarded and enforced**
- **Given** a skeptic `AgentConfig` with `TimeoutSecs: 2` and a `ChatCompleter` that blocks for 5 seconds
- **When** `invokeSkeptic` is called with a parent context
- **Then** the per-agent timeout fires (via context deadline), `Result.TrippedBudgets` contains `"timeout"` or similar, and the returned `Verification` has `Verdict: "unverifiable"`

**Scenario 4: Budgets within limits — normal verdict**
- **Given** a skeptic `AgentConfig` with `MaxTurns: 10`, `ToolBudgetBytes: 1000000`, `TimeoutSecs: 60`, and a `ChatCompleter` that returns a valid verdict on the first turn
- **When** `invokeSkeptic` is called
- **Then** no budgets are tripped, and the returned `Verification` has the parsed verdict (confirmed/refuted/unverifiable from the LLM)

## Edge Cases
**Edge Case 1: AgentConfig with unset budgets (nil pointers)**
- **Given** a skeptic `AgentConfig` with `MaxTurns: nil`, `ToolBudgetBytes: nil`, `TimeoutSecs: nil`
- **When** `invokeSkeptic` constructs the `Agent`
- **Then** defaults are applied: MaxTurns=10 (from `defaultMaxTurns` in loop.go), ToolBudgetBytes=0 (unlimited), TimeoutSecs=0 (use parent context deadline only)

**Edge Case 2: Multiple budgets tripped simultaneously**
- **Given** a skeptic with `MaxTurns: 1` and `TimeoutSecs: 1`, and a `ChatCompleter` that blocks
- **When** `invokeSkeptic` is called
- **Then** the timeout fires first (or max_turns), `Result.TrippedBudgets` contains at least one entry, and the `Verification.Notes` mentions the tripped budget(s)

**Edge Case 3: Zero MaxTurns**
- **Given** a skeptic `AgentConfig` with `MaxTurns: 0` (explicit)
- **When** `invokeSkeptic` constructs the `Agent`
- **Then** `invokeToolLoop` applies `defaultMaxTurns` (10) — zero is treated as "use default" per loop.go:83

## Error Conditions
**Error Scenario 1: Budget trip produces unverifiable, not error**
- **Given** a skeptic invocation where MaxTurns is tripped
- **When** `invokeSkeptic` processes the Result
- **Then** the function returns a valid `*Verification` with `Verdict: "unverifiable"` — NOT an `error`. Budget trips are runtime events, not programming errors.

## Performance Requirements
- **Response Time:** Budget forwarding adds no overhead beyond setting struct fields (< 1μs)
- **Throughput:** Budget enforcement is per-finding; each skeptic's loop is independent

## Security Considerations
- **Input Validation:** Budget values from `AgentConfig` are validated at registry load time (existing code). `invokeSkeptic` trusts the resolved config.
- **Resource Protection:** Budgets prevent runaway skeptic invocations from exhausting provider quotas or compute.

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** (1) AgentConfig with each budget field set individually, (2) AgentConfig with all budgets nil, (3) AgentConfig with multiple budgets set, (4) Fakes that trigger each budget type
**Mock/Stub Requirements:** `ChatCompleter` (mock — configurable to trigger max_turns / timeout), `toolDispatcher` (mock — configurable to trigger tool_budget_bytes)

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (`go test ./internal/verify/...`)
- [x] No linting errors (`go vet ./internal/verify/...`)
- [x] Build succeeds (`go build ./...`)

**Story-Specific:**
- [x] `MaxTurns` forwarded from `AgentConfig` to `Agent.MaxTurns`
- [x] `ToolBudgetBytes` forwarded from `AgentConfig` to `Agent.ToolBudgetBytes`
- [x] `TimeoutSecs` forwarded from `AgentConfig` to `Agent.TimeoutSecs`
- [x] Tripped budget recorded in `Verification.Notes`
- [x] Test for each budget type (max_turns, tool_budget_bytes, timeout) verifies `unverifiable` verdict

**Manual Review:**
- [x] Code reviewed and approved

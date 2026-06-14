# Acceptance Criteria: Live Status Counters

**Related User Story:** [05: Transcript & Accounting](../user-stories/05-transcript-accounting.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Counter fields | Go `int` and `int64` on `AgentStatus` struct | `Turns int`, `ToolCalls int`, `ToolBytes int64` |
| Serialization | `encoding/json` marshal of `AgentStatus` | Status file written once per agent at completion (budget trip, degrade, error, or normal finish) |
| Counter source | Same values driving budget enforcement (Story 2) | Single source of truth; no duplicate counting |
| Status writer | Existing `status.json` writer in `internal/fanout/status.go:249` | Extended to serialize new fields |
| Test framework | `go test` + `t.TempDir()` | Read `status.json` mid-run and after completion |

### Related Files (from codebase-discovery.json)

- `internal/fanout/status.go` — modify: ensure `AgentStatus` includes `Turns`, `ToolCalls`, `ToolBytes` fields with correct JSON tags
- `internal/fanout/status.go` — modify: serialize new fields in `status.json` writer
- `internal/fanout/engine.go` — modify: update counters after each turn and tool execution; write `status.json` once per agent at completion
- `internal/fanout/engine_test.go` — modify: verify counters in `status.json` match actual run counts
- `internal/fanout/status_test.go` — modify: verify JSON serialization includes new fields

## Happy Path Scenarios

**Scenario 1: Counters reflect actual usage at any halt point**
- **Given** a tool-using agent executing a 4-turn session
- **When** an external reader checks `status.json` after the run completes
- **Then** `status.json` shows `turns: 4`, `tool_calls: <count of calls in turns 1-4>`, `tool_bytes: <sum of result bytes in turns 1-4>`
- **And** the values match the actual counts from the engine's execution

**Scenario 2: Counters finalized on normal completion**
- **Given** a tool-using agent that completes after 3 turns with 5 total tool calls and 12,000 total tool bytes
- **When** the run completes
- **Then** `status.json` shows `turns: 3`, `tool_calls: 5`, `tool_bytes: 12000`
- **And** the counters do not change after completion

**Scenario 3: Counters finalized on budget trip**
- **Given** a tool-using agent with a tool-byte budget of 20,000
- **And** the agent accumulates 21,000 bytes across 3 turns with 7 tool calls
- **When** the budget trips after turn 3
- **Then** `status.json` shows `turns: 3`, `tool_calls: 7`, `tool_bytes: 21000`
- **And** `tripped_budgets` includes the tool-byte budget
- **And** the counters reflect the actual usage that triggered the trip

**Scenario 4: Counters finalized on degradation**
- **Given** a tool-using agent that degrades to single-shot after the provider rejects tool definitions
- **And** the agent completed 1 turn with 0 tool calls before degrading
- **When** the degraded run completes
- **Then** `status.json` shows `turns: 1`, `tool_calls: 0`, `tool_bytes: 0`
- **And** the agent's degraded status is recorded

**Scenario 5: Counters for single-shot agent (Tools=false)**
- **Given** a non-tool agent (single-shot, `Tools=false`)
- **When** the run completes
- **Then** `status.json` shows `turns: 1`, `tool_calls: 0`, `tool_bytes: 0`
- **And** no tool-related counters are meaningful for this agent

## Edge Cases

**Edge Case 1: Zero-byte tool results**
- **Given** all tool results in a run return 0 bytes
- **When** the run completes
- **Then** `tool_bytes` is 0
- **And** `tool_calls` reflects the actual number of calls made

**Edge Case 2: Provider error on first turn**
- **Given** the provider returns an error on the very first Chat call
- **When** the error is handled
- **Then** `status.json` shows `turns: 0` or `turns: 1` (depending on whether the failed attempt counts) — implementation must define this consistently
- **And** `tool_calls: 0`, `tool_bytes: 0`

**Edge Case 3: Large tool_bytes exceeding int32 range**
- **Given** a long-running agent that accumulates 3 billion bytes of tool results
- **When** the run completes
- **Then** `tool_bytes` is recorded as the exact int64 value (3,000,000,000)
- **And** the JSON serialization does not truncate or overflow

**Edge Case 4: Context cancellation mid-tool-execution**
- **Given** an agent running turn 3 of 5 with 2 tool calls in progress
- **And** the context is cancelled after 1 of 2 tool results is recorded
- **When** the cancellation propagates
- **Then** `status.json` reflects the counters up to the last fully-completed tool execution
- **And** partial tool results from the cancelled execution are not counted (or counted consistently per implementation)

## Error Conditions

**Error Scenario 1: Status file write fails**
- **Given** the status writer encounters an I/O error writing `status.json`
- **When** the error occurs
- **Then** the error is logged
- **And** the agent loop continues (status write failure does not fail the review)
- **And** the counters are still updated in memory for the final status write attempt

**Error Scenario 2: Backward compatibility — old reader encounters new fields**
- **Given** a 1.x status reader that does not know about `turns`, `tool_calls`, `tool_bytes`
- **When** it reads a `status.json` produced by 2.0
- **Then** the reader ignores the unknown fields (standard JSON unmarshaling behavior)
- **And** all existing 1.x fields are still present and unchanged

## Performance Requirements

- **Status write frequency:** Status file is written once per agent at completion; no per-turn writes
- **Write size:** `status.json` is small (< 1KB per agent); marshal and write completes in under 1ms
- **Counter overhead:** Counter increments are integer additions; negligible CPU overhead

## Security Considerations

- **No counter manipulation:** Counters are derived from engine execution state, not from user input; no external actor can influence counter values
- **Atomic status writes:** Status file should be written atomically (write to temp, rename) to prevent partial reads by concurrent `atcr status` commands
- **No sensitive data in counters:** Counters are pure integers; no tool result content is embedded in `status.json`

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Scripted multi-turn agent runs with known tool call counts and byte sizes
- Budget-tripped runs with known counter values at trip time
- Degraded runs and single-shot runs
- Context-cancelled runs

**Mock/Stub Requirements:**
- Fake `Completer` returning scripted tool calls with controlled result byte sizes
- `t.TempDir()` for `status.json` output
- Helper to read and parse `status.json` at specific points during execution

**Test Cases to Implement:**
1. Counters finalized: read `status.json` after completion and verify values match the run
2. Counters finalized on normal completion: verify exact match with known run stats
3. Counters finalized on budget trip: verify `tool_bytes` exceeds budget threshold
4. Counters finalized on degradation: verify counters reflect pre-degradation state
5. Single-shot agent (`Tools=false`): verify counters are 0/1 as appropriate
6. Zero-byte tool results: verify `tool_bytes: 0` with non-zero `tool_calls`
7. Large `tool_bytes` (> 2^31): verify int64 serialization
8. Backward compatibility: verify 1.x fields still present in status JSON
9. Provider error on first turn: verify counter state

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] JSON serialization tests pass for new fields

**Story-Specific:**
- [ ] `AgentStatus` struct includes `Turns int`, `ToolCalls int`, `ToolBytes int64` with JSON tags `turns`, `tool_calls`, `tool_bytes`
- [ ] Counters are updated from the same values driving budget enforcement (single source of truth)
- [ ] `status.json` is written once per agent at completion with final counter values
- [ ] Counters are finalized for all completion paths: normal, budget-tripped, degraded, error
- [ ] Backward compatibility: 1.x fields in `status.json` are unchanged

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Counter update logic reviewed for consistency with budget enforcement (Story 2)
- [ ] Status write atomicity reviewed for concurrent read safety

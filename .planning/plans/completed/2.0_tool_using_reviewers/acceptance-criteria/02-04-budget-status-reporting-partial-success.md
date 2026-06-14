# Acceptance Criteria: Budget Status Reporting and Partial Success

**Related User Story:** [02: Budget Enforcement](../user-stories/02-budget-enforcement.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Status struct | Go struct `AgentStatus` in `internal/fanout/status.go` | Gains `TrippedBudgets`, `Turns`, `ToolCalls`, `ToolBytes` fields |
| Counter tracking | Go int/int64 variables in agent loop | Accumulated during loop, written to status on completion |
| JSON serialization | `encoding/json` with struct tags | `tripped_budgets`, `turns`, `tool_calls`, `tool_bytes` in status.json |
| Partial-success flag | Existing `ReviewStatus.Partial bool` | Budget-tripped agents still produce results; reconcile consumes them normally |
| Atomic file write | `atomicWriteFile` in `status.go` | status.json written atomically regardless of outcome |
| Test framework | `go test` + JSON parsing of written status files |

### Related Files (from codebase-discovery.json)

- `internal/fanout/status.go:225` — modify: add `TrippedBudgets []string` field to `AgentStatus`; ensure `Turns`, `ToolCalls`, `ToolBytes` are populated
- `internal/fanout/status.go:249` — modify: `WriteStatus` serializes new fields; `omitempty` on `TrippedBudgets` so 1.x status.json is unchanged when no budgets tripped
- `internal/fanout/engine.go:228` — modify: agent loop populates counter fields and tripped-budgets slice into `Result` before returning
- `internal/fanout/engine.go:228` — modify: on any halt path (budget trip, timeout, error), counters are written unconditionally
- `internal/fanout/engine_test.go` — create/modify: tests verifying counter accuracy across all halt paths
- `internal/fanout/status_test.go` — create/modify: tests verifying JSON serialization of new fields

## Happy Path Scenarios

**Scenario 1: All counters recorded on normal completion**
- **Given** a tool-using agent that completes 4 turns with 6 tool calls totaling 2500 bytes of tool results
- **And** no budgets are tripped
- **When** the agent loop finishes and status.json is written
- **Then** `status.json` contains: `"turns": 4, "tool_calls": 6, "tool_bytes": 2500`
- **And** `status.json` does NOT contain `"tripped_budgets"` (omitempty — absent when empty)
- **And** `"status": "ok"`

**Scenario 2: Multiple budgets tripped in one run (byte budget + timeout)**
- **Given** an agent with `MaxTurns=10`, `ToolBudgetBytes=500`, and `TimeoutSecs=10`
- **And** on turn 3, tool results push cumulative bytes to 600 (tools ran because `turns=3 < MaxTurns=10`)
- **And** a timeout expires while `requestFinalAnswer` awaits the model's response after the byte-budget trip
- **When** the agent loop halts
- **Then** `status.json` contains: `"tripped_budgets": ["tool_budget_bytes", "timeout_secs"]`
- **And** counters reflect actual usage: `"turns": 3, "tool_calls": <actual>, "tool_bytes": 600`
- **And** `"status": "timeout"` (timeout recorded by `requestFinalAnswer` at `loop.go:270` when the Chat call returns a deadline error)

**Scenario 3: Single budget tripped — partial success**
- **Given** an agent with `MaxTurns=5` and `Tools=true`
- **And** the agent hits turn limit on turn 5
- **When** the engine requests a final answer and the model responds
- **Then** the result has `"status": "ok"` (final answer was produced)
- **And** `status.json` contains: `"tripped_budgets": ["max_turns"]`
- **And** the review is consumed by reconcile identically to a fully-completed agent
- **And** `ReviewStatus.Partial` may be set depending on reconcile logic

**Scenario 4: Counters recorded on provider error (no budget trip)**
- **Given** an agent with `MaxTurns=10`
- **And** the provider returns a transport error on turn 2
- **When** the error is handled
- **Then** `status.json` contains: `"turns": 2, "tool_calls": <actual>, "tool_bytes": <actual>`
- **And** `tripped_budgets` is absent (no budget was tripped — this was a provider failure)
- **And** `"status": "failed"`

## Edge Cases

**Edge Case 1: Zero turns executed**
- **Given** an agent with `TimeoutSecs=1` and the provider takes 2 seconds
- **When** the first `Complete` call times out
- **Then** `status.json` contains: `"turns": 0, "tool_calls": 0, "tool_bytes": 0`
- **And** `"tripped_budgets": ["timeout_secs"]`
- **And** `"status": "timeout"`

**Edge Case 2: Non-tool-using agent (Tools=false)**
- **Given** an agent with `Tools=false` (single-shot, 1.x behavior)
- **When** the agent completes
- **Then** `status.json` does NOT contain `turns`, `tool_calls`, or `tool_bytes` fields (all are nil/omitempty)
- **And** `tripped_budgets` is absent
- **And** the status.json format is identical to 1.x (backward compatible)

**Edge Case 3: Degrade path (non-tool-capable model fallback)**
- **Given** a tool-using agent whose provider does not support tool use
- **And** the engine falls back to a non-tool-capable model (degrade path)
- **When** the fallback agent completes with a single-shot response
- **Then** `status.json` contains: `"turns": 0, "tool_calls": 0, "tool_bytes": 0` (no tool loop ran)
- **And** counters are written unconditionally (not skipped on the degrade path)
- **And** `"status": "ok"` (fallback succeeded)

**Edge Case 4: Counters on context cancellation (SIGINT)**
- **Given** an agent on turn 4 with cumulative tool_bytes = 1200
- **And** the user sends SIGINT
- **When** the context is cancelled
- **Then** `status.json` contains the partial counters: `"turns": <completed>, "tool_calls": <actual>, "tool_bytes": 1200`
- **And** `"tripped_budgets"` includes `"timeout_secs"` (context cancellation maps to timeout)

## Error Conditions

**Error Scenario 1: status.json write fails after budget trip**
- **Given** an agent loop that tripped `max_turns`
- **And** the disk is full or the output directory is not writable
- **When** `WriteStatus` attempts to write
- **Then** `WriteStatus` returns an error: `"failed to write status.json for agent '<name>': ..."`
- **And** the error propagates to the caller (no silent failure)

**Error Scenario 2: Corrupt status.json from a previous run**
- **Given** a status.json file that is corrupt (truncated JSON)
- **When** `ReadReviewStatus` attempts to parse it
- **Then** an error is returned: `"summary.json is corrupt: ..."` (per existing behavior)
- **And** the new fields do not introduce any new parsing failure modes (they are standard JSON types)

## Performance Requirements

- **Counter accumulation:** O(1) per tool call (integer addition); negligible impact on loop performance
- **status.json size:** 4 additional fields add < 100 bytes to the JSON output; no impact on I/O
- **Write frequency:** status.json is written once per agent (at completion), not per turn — no additional disk I/O

## Security Considerations

- **Counter integrity:** Counters are computed internally by the engine, not from model-reported values; the model cannot manipulate them
- **No sensitive data in counters:** `turns`, `tool_calls`, `tool_bytes` are integer metrics — no file paths, content, or secrets leak through status.json
- **Backward compatibility:** `omitempty` on new fields ensures 1.x consumers of status.json are not broken by the addition of new fields
- **Atomic writes:** `atomicWriteFile` (temp + rename) ensures status.json is never partially written, even when recording budget trips

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Fake `Completer` returning scripted sequences of tool calls and final answers with known sizes
- `AgentConfig` fixtures with various budget combinations
- Expected `status.json` JSON strings for assertion

**Mock/Stub Requirements:**
- Fake `Completer` that records call count and returns predetermined tool-call / final-answer sequences
- Temporary directory for status.json output (use `t.TempDir()`)
- Parse `status.json` after write and assert field values

**Test Cases to Implement:**
1. Counters (turns, tool_calls, tool_bytes) accurate after normal completion
2. Counters accurate after single budget trip (max_turns)
3. Multiple budgets tripped: `tripped_budgets` array contains all tripped names
4. Counters accurate after provider error (no budget trip)
5. Counters written on degrade path (non-tool fallback)
6. Zero-turn scenario: all counters = 0, timeout in tripped_budgets
7. Non-tool agent (Tools=false): no counter fields in status.json (omitempty)
8. Context cancellation: partial counters recorded
9. `WriteStatus` error propagation on disk failure
10. JSON backward compatibility: 1.x-style status.json (no tool fields) still valid

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)
- [ ] JSON serialization tests confirm status.json schema correctness

**Story-Specific:**
- [ ] `AgentStatus.TrippedBudgets []string` records all tripped budget names (e.g., `"max_turns"`, `"tool_budget_bytes"`, `"timeout_secs"`)
- [ ] `AgentStatus.Turns`, `ToolCalls`, `ToolBytes` equal the actual usage in every halt path (normal, budget trip, timeout, error, degrade)
- [ ] Counters written unconditionally — including on the degrade path and error path
- [ ] New fields use `omitempty` so 1.x status.json format is unchanged for non-tool agents
- [ ] Partial-success semantics hold: budget-tripped agent produces a result consumed identically by reconcile

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] status.json output manually inspected for a tool-using agent run
- [ ] Backward compatibility confirmed: existing 1.x status.json consumers unaffected
- [ ] Reconcile tested with a budget-tripped agent result — produces correct verdict

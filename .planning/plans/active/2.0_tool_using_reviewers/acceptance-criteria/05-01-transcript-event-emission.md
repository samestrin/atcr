# Acceptance Criteria: Transcript Event Emission

**Related User Story:** [05: Transcript & Accounting](../user-stories/05-transcript-accounting.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| JSONL writer | Go struct wrapping `os.File` with `bufio.Writer` | `Open`, `RecordToolCalls`, `RecordToolResults`, `RecordFinal`, `Close` methods |
| Event schema | `encoding/json` marshaling per event type | One JSON object per line, stable field set per event kind |
| Truncation detection | Byte-length check against per-call cap | `truncated: true` + `original_bytes` when result exceeds cap |
| Best-effort error handling | Engine structured logger (`slog` or equivalent) | Write errors logged and swallowed; never propagated to agent loop |
| Test framework | `go test` + `t.TempDir()` | Write transcript to temp dir, read back and assert event sequence |

### Related Files (from codebase-discovery.json)

- `internal/tools/transcript.go` — create: JSONL writer with `Open`, `RecordToolCalls`, `RecordToolResults`, `RecordFinal`, `Close` methods
- `internal/tools/transcript_test.go` — create: unit tests for event emission, truncation markers, schema correctness
- `internal/fanout/engine.go:228` — modify: invoke transcript writer at each loop boundary (after Chat returns tool_calls, after each tool execution, after final message)
- `internal/fanout/engine_test.go` — modify: integration tests verifying transcript events match the engine's Chat call sequence
- `internal/fanout/artifacts.go:160` — reference: `writeAgentArtifacts` writes `transcript.jsonl` under `raw/<agent>/`

## Happy Path Scenarios

**Scenario 1: Transcript records tool_calls event**
- **Given** a tool-using agent that receives a `Chat` response containing two tool calls
- **When** the engine processes the Chat response
- **Then** the transcript writer emits one JSON line: `{"event":"tool_calls","turn":1,"ts":"<RFC3339>","tool_calls":[{"id":"call_1","name":"read_file","arguments":{...}},{"id":"call_2","name":"grep","arguments":{...}}]}`
- **And** the line is valid JSON parseable by `encoding/json`

**Scenario 2: Transcript records tool_result event within cap**
- **Given** a tool execution returns a result of 500 bytes (below the per-call byte cap)
- **When** the tool result is recorded
- **Then** the transcript emits: `{"event":"tool_result","turn":1,"ts":"<RFC3339>","tool_call_id":"call_1","name":"read_file","content":"<result>","truncated":false,"original_bytes":500}`
- **And** `content` contains the full result verbatim

**Scenario 3: Transcript records truncated tool_result**
- **Given** a tool execution returns a result of 50,000 bytes (above the per-call byte cap of e.g. 32,000)
- **When** the tool result is recorded
- **Then** the transcript emits the event with `content` truncated to the cap
- **And** `truncated` is set to `true`
- **And** `original_bytes` is set to 50000
- **And** the truncated `content` matches exactly what was sent to the model

**Scenario 4: Transcript records final message**
- **Given** an agent loop completes with a final assistant message on turn 4
- **When** the final message is received
- **Then** the transcript emits: `{"event":"final","turn":4,"ts":"<RFC3339>","message":"<final answer text>"}`
- **And** this is the last line in the transcript file

**Scenario 5: Full multi-turn transcript sequence**
- **Given** a tool-using agent that executes 3 turns with tool calls and results each, then a final answer
- **When** the full run completes
- **Then** `transcript.jsonl` contains exactly: tool_calls (turn 1), tool_result(s) (turn 1), tool_calls (turn 2), tool_result(s) (turn 2), tool_calls (turn 3), tool_result(s) (turn 3), final (turn 3)
- **And** each line is a valid JSON object with `event`, `turn`, and `ts` fields
- **And** the sequence is a faithful record of the engine's Chat call sequence

## Edge Cases

**Edge Case 1: Agent completes with no tool calls (single turn)**
- **Given** a tool-using agent whose first Chat response contains no tool calls (immediate final answer)
- **When** the run completes
- **Then** the transcript contains a single line: the `final` event for turn 1
- **And** no `tool_calls` or `tool_result` events are emitted

**Edge Case 2: Multiple tool calls in a single turn**
- **Given** a Chat response returns 3 tool calls in one turn
- **When** the events are recorded
- **Then** one `tool_calls` event is emitted containing all 3 tool calls in its `tool_calls` array
- **And** 3 separate `tool_result` events are emitted (one per tool call), each with its own `tool_call_id`

**Edge Case 3: Empty tool result**
- **Given** a tool execution returns an empty string result (0 bytes)
- **When** the tool result is recorded
- **Then** the transcript emits the event with `content: ""`, `truncated: false`, `original_bytes: 0`

**Edge Case 4: Tool result exactly at byte cap**
- **Given** a tool result whose byte length equals the per-call cap exactly
- **When** the tool result is recorded
- **Then** `truncated` is `false` (at cap, not above it)
- **And** `content` contains the full result

## Error Conditions

**Error Scenario 1: Transcript write fails (disk full or I/O error)**
- **Given** the transcript writer encounters an I/O error on `Write`
- **When** the error occurs mid-run
- **Then** the error is logged via the engine's structured logger with the agent name and event type
- **And** the error is NOT propagated to the agent loop
- **And** the agent loop continues executing normally
- **And** the review result is produced successfully despite the incomplete transcript

**Error Scenario 2: Transcript file cannot be created**
- **Given** the `raw/<agent>/` directory does not exist or is not writable
- **When** `Open(path)` is called
- **Then** the error is logged
- **And** the transcript writer returns a no-op writer (or nil-safe handle) that silently discards subsequent `Record*` calls
- **And** the agent loop proceeds without transcript recording

**Error Scenario 3: JSON marshaling fails for an event**
- **Given** an event payload contains unmarshalable data (e.g., a channel or function in arguments)
- **When** the writer attempts to marshal the event
- **Then** the error is logged with the event type and turn number
- **And** the event is skipped (not written)
- **And** subsequent events continue to be recorded normally

## Performance Requirements

- **Write latency:** Each event write is a single buffered JSON line (~100-500 bytes); buffered writer flushes per turn, not per event, keeping I/O overhead under 1ms per turn
- **No blocking:** Transcript writes do not block the agent loop; the buffered writer absorbs small writes without syscall per event
- **Memory:** Writer holds a `bufio.Writer` (4KB default buffer) per active agent; negligible for typical roster sizes

## Security Considerations

- **No additional redaction:** Transcript records tool results verbatim (truncated to cap); the path jail (Story 3) already prevents `.git/` and out-of-root reads, so no additional content filtering is needed
- **Append-only:** File opened with `O_CREATE|O_WRONLY|O_APPEND`; no mid-file rewrites means no risk of partial-line corruption from concurrent writers
- **No secrets injection:** Transcript does not receive or record API keys, tokens, or authentication headers; only tool call arguments and results

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Scripted tool results of various sizes: 0 bytes, 500 bytes, exactly at cap, above cap (e.g., 50KB)
- Multi-tool-call Chat responses (1, 2, 3 tool calls per turn)
- Multi-turn sequences (1 turn, 3 turns, 5 turns)

**Mock/Stub Requirements:**
- Fake tool executor that returns predetermined results with controlled byte sizes
- Fake `Completer` that returns scripted tool calls then a final answer
- `t.TempDir()` for transcript file output

**Test Cases to Implement:**
1. Emit `tool_calls` event and verify JSON schema
2. Emit `tool_result` within cap — verify `truncated: false` and full content
3. Emit `tool_result` above cap — verify `truncated: true`, `original_bytes`, and truncated content
4. Emit `final` event — verify it is the last line
5. Full multi-turn sequence — verify event order matches engine's Chat call sequence
6. Write failure — inject failing writer, verify error is logged and loop continues
7. File creation failure — verify no-op behavior and loop continuation
8. Empty tool result — verify `original_bytes: 0` and `truncated: false`

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/tools/...`)
- [ ] All integration tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `tool_calls` events contain all tool calls from the Chat response with correct `id`, `name`, `arguments`
- [ ] `tool_result` events include `truncated` flag and `original_bytes` field
- [ ] Truncated results have `content` matching what was sent to the model
- [ ] Write errors are logged and do not fail the agent loop

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Transcript JSON schema reviewed for operator readability and grep-ability
- [ ] Best-effort error handling verified with a live failing-writer test

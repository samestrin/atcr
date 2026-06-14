# Acceptance Criteria: Transcript Durability and Replay

**Related User Story:** [05: Transcript & Accounting](../user-stories/05-transcript-accounting.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Append-only file | `os.OpenFile` with `O_CREATE\|O_WRONLY\|O_APPEND` | Kernel-level append semantics; no mid-file rewrites |
| Buffered writer | `bufio.Writer` wrapping file | Flush per turn boundary, not per event |
| Crash recovery | JSONL line-by-line parsing | Each line is independently valid; partial last line ignored on read |
| Replay test harness | Go test helper in `_test.go` | Reads `transcript.jsonl`, replays event sequence, asserts equivalence with engine trace |
| Test framework | `go test` + crash simulation | Kill process mid-write or inject write failure after N events |

### Related Files (from codebase-discovery.json)

- `internal/tools/transcript.go` — modify: ensure `Open` uses `O_CREATE|O_WRONLY|O_APPEND`; flush semantics at turn boundaries
- `internal/tools/transcript_test.go` — modify: tests for append-only behavior, partial transcript validity, and replay equivalence
- `internal/tools/replay_test.go` — create: replay test helper that reads `transcript.jsonl` and asserts it matches the recorded engine Chat trace
- `internal/fanout/engine_test.go` — modify: integration tests that crash mid-run and verify partial transcript is replayable
- `internal/fanout/status.go:275` — reference: `atomicWriteFile` pattern for atomic writes (used elsewhere; transcript uses append-only)

## Happy Path Scenarios

**Scenario 1: Append-only write preserves prior events**
- **Given** a transcript file with 5 events from turns 1-2
- **When** turn 3 events are written
- **Then** the file contains all 5 original events plus the new turn 3 events
- **And** no existing lines are modified or overwritten
- **And** each line remains independently parseable JSON

**Scenario 2: Crashed run leaves valid partial transcript**
- **Given** an agent run that is interrupted (context cancelled) after recording 4 of 6 expected events
- **When** the operator reads `transcript.jsonl`
- **Then** the file contains exactly 4 valid JSON lines
- **And** each line parses successfully with `encoding/json`
- **And** the events form a coherent partial session (tool_calls, tool_results for turns 1-2)
- **And** the replay harness can process the 4 events without error

**Scenario 3: Replay harness reconstructs Chat call sequence**
- **Given** a completed agent run with a known scripted provider
- **And** the engine recorded its Chat call trace (request tool_calls, tool results, final message)
- **When** the replay harness reads `transcript.jsonl`
- **Then** the replayed event sequence matches the engine's recorded trace exactly
- **And** each `tool_calls` event matches the corresponding Chat request's tool calls
- **And** each `tool_result` event matches the corresponding tool execution result
- **And** the `final` event matches the last Chat response content

**Scenario 4: Per-turn flush ensures durability**
- **Given** an agent executing turn 3 of 5
- **When** the turn completes and the writer flushes
- **Then** all events for turns 1-3 are durable on disk (visible to a concurrent reader)
- **And** events for turn 4+ are not yet visible

## Edge Cases

**Edge Case 1: Interrupt mid-flush**
- **Given** the buffered writer is flushing turn 3 events
- **And** the process is killed (SIGKILL) during the flush syscall
- **When** the operator reads the transcript
- **Then** the file contains complete lines for turns 1-2
- **And** turn 3 may have 0 or more complete lines (depending on where the kill landed)
- **And** there is no partial JSON line at the end (buffered writer writes complete lines, or a partial line that the reader skips)
- **And** all complete lines are valid JSON

**Edge Case 2: Concurrent readers during active write**
- **Given** an agent is actively writing transcript events
- **And** an operator reads the file concurrently (e.g., `tail -f` or `atcr status --follow`)
- **When** the reader encounters the current end of file
- **Then** the reader sees only complete JSON lines
- **And** no partial lines are visible (append writes are line-atomic for reasonable line sizes on Linux/macOS)

**Edge Case 3: Transcript for a degraded agent**
- **Given** an agent that started with `tools: true` but degraded to single-shot
- **When** the run completes
- **Then** the transcript contains the events from the tool-using turns before degradation
- **And** the transcript ends with either a `final` event or the last event before degradation
- **And** the replay harness can process the partial session

**Edge Case 4: Empty transcript (agent fails before first event)**
- **Given** an agent that fails during the first Chat call (provider error)
- **When** the operator reads `transcript.jsonl`
- **Then** the file exists but is empty (0 bytes) or does not exist
- **And** the replay harness handles this gracefully (empty session, no events to replay)

## Error Conditions

**Error Scenario 1: Replay encounters malformed JSON line**
- **Given** a transcript file with a corrupted line (e.g., partial write from crash)
- **When** the replay harness reads the file
- **Then** the harness logs a warning with the line number and content
- **And** skips the malformed line
- **And** continues processing subsequent valid lines
- **And** reports the total valid events replayed vs. total lines

**Error Scenario 2: Replay encounters missing required fields**
- **Given** a transcript line that is valid JSON but missing required fields (e.g., no `event` field)
- **When** the replay harness reads the line
- **Then** the harness logs a warning identifying the line number and missing field
- **And** skips the line
- **And** continues processing

**Error Scenario 3: Replay transcript from a different engine version**
- **Given** a transcript produced by a different version of the engine with additional event types
- **When** the replay harness reads the file
- **Then** unknown event types are logged and skipped
- **And** known event types are processed normally
- **And** the harness does not crash or error on unknown fields

## Performance Requirements

- **Flush frequency:** Buffered writer flushes once per turn boundary; for a 5-turn agent, that is 5 flushes total (not per-event)
- **Read performance:** Replay harness reads the file sequentially; for a 1MB transcript (~2000 events), read and parse completes in under 50ms
- **No fsync per event:** Durability is turn-level, not event-level; fsync (if used) fires per turn flush, not per event

## Security Considerations

- **Append-only guarantee:** `O_APPEND` flag ensures kernel-level append semantics; even if the engine crashes, existing events cannot be overwritten
- **No transcript tampering:** Transcript is written by the engine process only; file permissions are inherited from the `raw/` directory (owner read/write only)
- **Replay is read-only:** The replay harness opens the file with `O_RDONLY`; it never modifies the transcript

## Test Implementation Guidance

**Test Type:** UNIT + INTEGRATION

**Test Data Requirements:**
- Complete transcript files from scripted multi-turn runs (3-5 turns)
- Partial transcript files simulating crash mid-run (truncate a complete transcript after N lines)
- Malformed transcript files with corrupted lines, missing fields, unknown event types
- Empty transcript files (0 bytes)

**Mock/Stub Requirements:**
- Engine Chat trace recorder that captures the exact sequence of Chat requests and tool results
- Replay test helper function: `func ReplayTranscript(t *testing.T, path string, expectedTrace []ChatTrace) bool`
- Crash simulation: inject a writer that fails after N writes, then verify partial transcript

**Test Cases to Implement:**
1. Append-only: write events in two batches, verify all lines present and none modified
2. Crash simulation: kill writer after 4 of 6 events, verify partial transcript is valid JSONL
3. Replay equivalence: run scripted agent, replay transcript, assert match with engine trace
4. Malformed line handling: inject a corrupted line, verify replay skips it and continues
5. Missing fields: inject a JSON line missing `event` field, verify replay handles gracefully
6. Unknown event type: inject a line with `event: "unknown_future_type"`, verify replay skips
7. Empty transcript: verify replay handles 0-byte file without error
8. Degraded agent transcript: verify replay handles session that ends mid-tool-call

## Definition of Done

**Auto-Verified:**
- [ ] All unit tests pass (`go test ./internal/tools/...`)
- [ ] All integration tests pass (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Transcript file opened with `O_CREATE|O_WRONLY|O_APPEND`
- [ ] Buffered writer flushes at turn boundaries, not per event
- [ ] Partial transcript from simulated crash is valid JSONL (every line parseable)
- [ ] Replay harness reconstructs exact Chat call sequence from transcript for all scripted test runs
- [ ] Replay harness handles malformed lines, missing fields, and unknown event types without crashing

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Crash simulation test reviewed for realistic failure modes
- [ ] Replay harness reviewed for divergence risk with engine Chat call construction

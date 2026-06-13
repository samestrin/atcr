# Acceptance Criteria: Loop Hygiene - Repeated Calls and Malformed JSON

**Related User Story:** [01: Agent Loop Execution](../user-stories/01-agent-loop-execution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Dedup Detection | Go map or hash comparison of `(Function.Name, Function.Arguments)` pairs | In-memory per-loop |
| Nudge Injection | Synthetic `role:"user"` or `role:"system"` message appended to history | Before next `Chat` call |
| Error Recovery | Tool error returned as `role:"tool"` result with error message content | Dispatcher-level |
| Test Framework | `go test` + `net/http/httptest` | Scripted providers emitting repeats and malformed JSON |
| Key Dependencies | `encoding/json`, `fmt` (stdlib) | |

## Related Files
- `internal/fanout/engine.go` - modify: repeated tool call detection, nudge injection, malformed JSON retry logic
- `internal/fanout/engine_test.go` - create: tests for nudge-on-repeat, halt-on-second-repeat, malformed JSON retry, second-malformed halt
- `internal/tools/dispatch.go` - consume: tool errors returned by dispatcher as non-fatal results

## Happy Path Scenarios
**Scenario 1: Identical repeated tool call triggers nudge**
- **Given** a model returns the exact same `tool_call` (same `Function.Name` and `Function.Arguments`) as the previous turn
- **When** the loop detects the repeat
- **Then** a nudge message is injected into the conversation history (e.g., "You already called this tool with these arguments. Please provide your final answer."); the tool is NOT re-executed; the loop continues for one more turn

**Scenario 2: Second identical repeat halts the loop**
- **Given** a nudge has already been injected for a repeated tool call, and the model returns the same tool call again
- **When** the loop detects the second repeat
- **Then** the loop halts; a final-answer request is injected as the assistant's response content; `Result.Turns` reflects the halt point

**Scenario 3: Malformed tool-call JSON returns tool error with one retry**
- **Given** a model returns a `tool_call` where `Function.Arguments` is not valid JSON
- **When** the dispatcher attempts to parse the arguments
- **Then** a `role:"tool"` message is returned with content: `"error: invalid JSON in tool arguments: <parse error>"`; the model receives the error and can retry on the next turn

**Scenario 4: Second consecutive malformed JSON halts with final answer request**
- **Given** the model has already received one malformed-JSON tool error, and returns another malformed `tool_call`
- **When** the loop detects the second consecutive malformed call
- **Then** the loop halts; a message requesting a final answer is appended

## Edge Cases
**Edge Case 1: Different tool calls are not flagged as repeats**
- **Given** turn N calls `read_file({path: "a.go"})` and turn N+1 calls `read_file({path: "b.go"})`
- **When** the dedup check runs
- **Then** the calls are NOT considered identical (different arguments); no nudge is injected

**Edge Case 2: Same function name but different arguments is not a repeat**
- **Given** turn N calls `grep({pattern: "foo"})` and turn N+1 calls `grep({pattern: "bar"})`
- **When** the dedup check runs
- **Then** not considered identical; both execute normally

**Edge Case 3: Non-consecutive identical calls**
- **Given** turn N calls `read_file({path: "a.go"})`, turn N+1 calls `grep(...)`, turn N+2 calls `read_file({path: "a.go"})` again
- **When** the dedup check runs on turn N+2
- **Then** the check compares against the immediately previous turn's calls only; turn N+2 is NOT flagged as a repeat (the previous turn had a different call)

**Edge Case 4: Multiple tool calls in one turn, one is a repeat**
- **Given** turn N returns `[read_file(a.go), grep(foo)]` and turn N+1 returns `[read_file(a.go), list_files(/)]`
- **When** dedup checks each call from turn N+1 against turn N
- **Then** `read_file(a.go)` is flagged as a repeat (nudge for that specific call); `list_files(/)` executes normally

## Error Conditions
**Error Scenario 1: Tool execution returns error**
- **Given** a tool call executes but the dispatcher returns an error (e.g., file not found, path outside jail)
- **When** the error is processed
- **Then** the error is returned as a `role:"tool"` result with `content: "error: <message>"`; the loop continues; the model can adjust its next call

**Error Scenario 2: Unknown tool name in tool_call**
- **Given** a model returns a `tool_call` with `Function.Name: "nonexistent_tool"`
- **When** the dispatcher receives the call
- **Then** a `role:"tool"` result is returned with `content: "error: unknown tool: nonexistent_tool"`; the loop continues

## Performance Requirements
- **Response Time:** Dedup check is O(1) per tool call (hash comparison of previous call set)
- **Throughput:** Nudge and halt logic adds no allocation to the hot path (pre-allocated message strings)

## Security Considerations
- **Authentication/Authorization:** N/A — loop hygiene is an internal control, not an access boundary
- **Input Validation:** Malformed JSON is caught and reported; never passed to tool execution unvalidated

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Scripted provider sequences that: (a) return identical tool calls across turns, (b) return malformed JSON in `Function.Arguments`, (c) return mixed valid/invalid calls, (d) return non-repeating calls to verify no false positives
**Mock/Stub Requirements:** httptest mock provider with turn-by-turn scripted responses; mock dispatcher that returns configurable results/errors

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/...`)
- [ ] No linting errors (`go vet`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Identical repeated tool call triggers nudge on first occurrence
- [ ] Second identical repeat halts loop and injects final-answer request
- [ ] Malformed tool-call JSON returns tool error (not loop crash) with one retry
- [ ] Second consecutive malformed JSON halts loop
- [ ] Non-identical calls are NOT flagged as repeats (no false positives)
- [ ] Tool execution errors are returned as `role:"tool"` results, never fatal

**Manual Review:**
- [ ] Code reviewed and approved

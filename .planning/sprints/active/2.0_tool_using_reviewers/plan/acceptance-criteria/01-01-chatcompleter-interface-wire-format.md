# Acceptance Criteria: ChatCompleter Interface and Wire Format

**Related User Story:** [01: Agent Loop Execution](../user-stories/01-agent-loop-execution.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| Interface | Go interface (`ChatCompleter`) | Defined in `internal/fanout/engine.go` |
| Wire Format | OpenAI-compatible JSON (`tools`, `tool_calls`, `role:"tool"`) | Via `internal/llmclient/client.go` |
| Test Framework | `go test` + `net/http/httptest` | Scripted mock providers |
| Key Dependencies | `encoding/json`, `context` (stdlib only) | No third-party deps |

### Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:17` - create: `ChatCompleter` interface definition alongside existing `Completer`
- `internal/llmclient/client.go:155` - modify: extend `chatRequest` with `Tools` field
- `internal/llmclient/client.go:162` - modify: extend `chatResponse` with `ToolCalls` field
- `internal/llmclient/client.go:165` - modify: add `Chat` method alongside `Complete`
- `internal/fanout/engine_test.go` - create: interface conformance tests, type assertion tests
- `internal/llmclient/client_test.go` - modify: add tests for `Chat` method with tool wire format

## Happy Path Scenarios
**Scenario 1: ChatCompleter interface defined and implemented by llmclient.Client**
- **Given** a `ChatCompleter` interface with signature `Chat(ctx, inv, messages, tools) (*ChatResponse, error)`
- **When** `llmclient.Client` is type-asserted to `ChatCompleter`
- **Then** the assertion succeeds and `Chat` is callable

**Scenario 2: Chat request includes tool definitions in wire format**
- **Given** a `Chat` invocation with a `tools []ToolDef` parameter containing tool definitions
- **When** the request is serialized to JSON
- **Then** the request body contains a `"tools"` array with OpenAI function-calling JSON Schema format (`name`, `description`, `parameters`)

**Scenario 3: Chat response carries tool_calls**
- **Given** a mock provider returns a response with `tool_calls` in the assistant message
- **When** the response is deserialized into `ChatResponse`
- **Then** `ChatResponse.Message.ToolCalls` contains the parsed tool calls with `ID`, `Function.Name`, `Function.Arguments`

**Scenario 4: role:tool messages sent in conversation history**
- **Given** a conversation history containing a message with `Role:"tool"` and `ToolCallID`
- **When** serialized to the provider
- **Then** the JSON contains `{"role":"tool","content":"<result>","tool_call_id":"<id>"}`

## Edge Cases
**Edge Case 1: Empty tools array omitted from request**
- **Given** a `Chat` call with an empty or nil `tools` slice
- **When** the request is serialized
- **Then** the `"tools"` field is omitted from JSON (omitempty)

**Edge Case 2: ChatCompleter type assertion fails**
- **Given** an engine caller that receives a `Completer` that does not implement `ChatCompleter`
- **When** the engine performs a type assertion `completer.(ChatCompleter)`
- **Then** the assertion returns `ok=false` without panic

**Edge Case 3: Provider returns finish_reason "tool_calls" with empty tool_calls array**
- **Given** a mock provider returns `finish_reason: "tool_calls"` but `tool_calls` is empty
- **When** the response is parsed
- **Then** the engine treats it as a final message (no tools to execute)

## Error Conditions
**Error Scenario 1: Provider returns HTTP error during Chat**
- **Given** a mock provider returns HTTP 500
- **When** `Chat` is called
- **Then** `Chat` returns an error wrapping the HTTP status code

**Error Scenario 2: Response JSON is malformed**
- **Given** a mock provider returns invalid JSON
- **When** `Chat` attempts to deserialize
- **Then** `Chat` returns a JSON decode error

## Performance Requirements
- **Response Time:** `Chat` serialization/deserialization adds no more than 1ms overhead per call (excluding network I/O)
- **Throughput:** No shared mutable state in `Chat` — safe for concurrent agent invocations

## Security Considerations
- **Authentication/Authorization:** Uses existing `llmclient.Client` auth (API key in `Invocation`); no new auth surface
- **Input Validation:** Tool definitions validated as valid JSON Schema at construction time; message content is opaque to the engine

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:** Scripted httptest responses with tool_calls, role:tool messages, edge cases (empty tools, malformed JSON, HTTP errors)
**Mock/Stub Requirements:** httptest server returning scripted JSON responses; no external provider needed

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (`go test ./internal/fanout/... ./internal/llmclient/...`)
- [ ] No linting errors (`go vet`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] `ChatCompleter` interface exists in `internal/fanout/engine.go` with correct signature
- [ ] `llmclient.Client` implements `ChatCompleter`
- [ ] Wire format includes `tools` in request and parses `tool_calls` from response
- [ ] `role:"tool"` messages serialize to the expected JSON shape in conversation history

**Manual Review:**
- [ ] Code reviewed and approved

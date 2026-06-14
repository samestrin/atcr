# Acceptance Criteria: Tool Dispatcher — Routing and Per-Call Byte Caps

**Related User Story:** [07: Tool Definitions & Dispatcher](../user-stories/07-tool-definitions-dispatcher.md)

## Implementation Technology

| Component | Technology | Notes |
|-----------|------------|-------|
| Routing | Go map `map[string]handlerFunc` | Name-to-handler lookup |
| Argument parsing | `encoding/json` | Parses `Function.Arguments` string |
| Byte cap | `capResultBytes(content, limit)` helper | Truncates string to limit with marker |
| Error type | `ToolError` struct implementing `error` | Returned to agent loop as `role:"tool"` content |
| Test framework | `go test` | Mock handlers and jail |

### Related Files (from codebase-discovery.json)

- `internal/tools/dispatch.go` — create: `Dispatcher` and `Execute` method
- `internal/tools/defs.go` — create: `ToolDef` and `Tools()` helper
- `internal/tools/dispatch_test.go` — create: unit tests
- `internal/tools/jail.go` — read: `Jail.Resolve` used by dispatcher

## Happy Path Scenarios

**Scenario 1: Route `read_file` call to its handler**
- **Given** a `tool_call` with `Function.Name: "read_file"` and arguments `{"path":"a.go"}`
- **When** the dispatcher executes it
- **Then** the `read_file` handler is invoked
- **And** its result is returned wrapped in a `ToolResult`

**Scenario 2: Route `grep` call to its handler**
- **Given** a `tool_call` with `Function.Name: "grep"`
- **When** executed
- **Then** the `grep` handler is invoked

**Scenario 3: Route `list_files` call to its handler**
- **Given** a `tool_call` with `Function.Name: "list_files"`
- **When** executed
- **Then** the `list_files` handler is invoked

**Scenario 4: Per-call byte cap truncates large results**
- **Given** a handler that returns a 1,000-byte string and a per-call byte cap of 256
- **When** the dispatcher executes the call
- **Then** the returned `ToolResult.Content` is truncated to 256 bytes
- **And** `ToolResult.Truncated` is `true`
- **And** `ToolResult.OriginalBytes` is `1000`

## Edge Cases

**Edge Case 1: Unknown tool name**
- **Given** a `tool_call` with `Function.Name: "unknown_tool"`
- **When** executed
- **Then** the dispatcher returns a `ToolError`: `"unknown tool: unknown_tool"`

**Edge Case 2: Handler returns empty string**
- **Given** a handler that returns `""`
- **When** executed
- **Then** the dispatcher returns `ToolResult{Content: "", Truncated: false, OriginalBytes: 0}`

**Edge Case 3: Tool result exactly at byte cap**
- **Given** a handler returns a string of length exactly equal to the cap
- **When** executed
- **Then** `Truncated` is `false`
- **And** content is unchanged

**Edge Case 4: Malformed JSON arguments**
- **Given** a `tool_call` with `Function.Arguments: "not json"`
- **When** executed
- **Then** the dispatcher returns a `ToolError`: `"invalid arguments: ..."`

## Error Conditions

**Error Scenario 1: Jail rejects path argument**
- **Given** a `read_file` call with a path that escapes the snapshot root
- **When** the dispatcher calls `jail.Resolve`
- **Then** the jail error is returned as a `ToolError`
- **And** the handler is not invoked

**Error Scenario 2: Handler panics**
- **Given** a misbehaving handler that panics
- **When** the dispatcher executes it
- **Then** the panic is recovered
- **And** a `ToolError` is returned: `"tool execution failed: ..."`
- **And** the panic does not crash the agent loop

## Performance Requirements

- **Dispatch latency:** Map lookup and JSON parsing complete in <1ms per call.
- **No allocation on hot path:** Pre-allocated error strings for common cases.

## Security Considerations

- The dispatcher is the only code path that invokes handlers; handlers do not parse raw tool-call input.
- All path arguments go through `jail.Resolve` before handlers see them.
- Byte cap prevents the model from receiving unbounded tool results.
- Unknown tools are rejected rather than silently ignored.

## Test Implementation Guidance

**Test Type:** UNIT

**Test Data Requirements:**
- Mock handlers returning known strings
- Mock jail that accepts/rejects paths
- Tool calls with valid and invalid names
- Tool calls with valid and malformed arguments

**Mock/Stub Requirements:**
- `Dispatcher` initialized with mock handlers and jail
- Mock handler that panics

**Test Cases:**
1. Route each of the three v1 tools
2. Unknown tool name
3. Per-call byte cap truncation
4. Empty handler result
5. Malformed JSON arguments
6. Jail rejection
7. Handler panic recovery
8. Result exactly at cap

## Definition of Done

**Auto-Verified:**
- [ ] All tests pass (`go test ./internal/tools/...`)
- [ ] No linting errors (`go vet ./...`)
- [ ] Build succeeds (`go build ./...`)

**Story-Specific:**
- [ ] Dispatcher routes `read_file`, `grep`, `list_files` by name
- [ ] Unknown tools return structured errors
- [ ] Per-call byte cap truncates and marks results
- [ ] Handler panics are recovered and returned as tool errors

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Dispatcher is the sole handler invocation path

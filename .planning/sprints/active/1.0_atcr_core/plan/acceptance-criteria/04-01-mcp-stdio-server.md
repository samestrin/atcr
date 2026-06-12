# Acceptance Criteria: MCP Stdio Server Startup

**Related User Story:** [04: MCP Integration](../user-stories/04-mcp-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| MCP Server | Go `modelcontextprotocol/go-sdk` v1.6.1 | Generic MCP server with stdio transport |
| Transport | `mcp.StdioTransport` | Stdout reserved for protocol; stderr for logs |
| CLI Subcommand | Go `cobra` subcommand | `atcr serve` |
| Test Framework | `testify` (assert, require) | Table-driven tests |
| Test Transport | `mcp.InMemoryTransport` | In-process testing without stdio |

## Related Files
- `cmd/atcr/serve.go` - create: `atcr serve` cobra subcommand that starts MCP stdio server
- `internal/mcp/server.go` - create: MCP server construction, tool registration, transport setup
- `internal/mcp/server_test.go` - create: Unit and integration tests for server startup and transport
- `cmd/atcr/serve_test.go` - create: Integration tests for serve command lifecycle
- `internal/mcp/handlers.go` - create: thin handler functions for each of the 5 tools (`atcr_review`, `atcr_reconcile`, `atcr_report`, `atcr_range`, `atcr_status`)

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [MCP Server Implementation](../documentation/mcp-server.md) — Authoritative spec for `atcr serve`, `mcp.StdioTransport`, generic `mcp.AddTool` with typed args/result, `InMemoryTransport` for tests, stderr discipline, and the 5-tool tool table.
- [CLI Architecture](../documentation/cli-architecture.md) — `cmd.ErrOrStderr()` is the only safe writer in `atcr serve` mode; `cmd.OutOrStdout()` is forbidden because stdout is owned by the protocol.

### Spec alignment notes

- **Stderr discipline is non-negotiable in serve mode**: any human-readable log, debug message, or diagnostic print that escapes to stdout will corrupt the MCP protocol and disconnect the client. The slog logger is initialized to os.Stderr at serve startup; cobra command output uses `cmd.ErrOrStderr()` (which is os.Stderr in serve mode). Test with `InMemoryTransport` plus a stderr-buffer capture to assert no protocol leakage.
- **`mcp-go-sdk` version**: pinned to **v1.6.1** per `.planning/specifications/packages/registry.yaml`. Use the generic `mcp.AddTool` (not the older non-generic `Server.AddTool`).
- **InMemoryTransport is the test transport** — never `os/exec` the binary for unit tests; reserve that for end-to-end smoke tests.
- **Handshake** is initiated by the client; the server's `initialize` response advertises `serverInfo` (`name: "atcr"`, version from build) and the registered `tools` capability list.
- **No business logic in handlers** — handlers are thin wrappers that call into the same internal packages as the CLI commands (`internal/fanout`, `internal/reconcile`, `internal/gitrange`, `internal/report`). Per `mcp-server.md`.

## Happy Path Scenarios

**Scenario 1: `atcr serve` starts MCP stdio server and listens on stdin/stdout**
- **Given** the atcr binary is built and available in PATH
- **When** an MCP client launches `atcr serve` as a subprocess
- **Then** the server initializes a stdio transport bound to stdin/stdout
- **And** the server sends an MCP `initialize` response with protocol version and capabilities
- **And** all human-readable log output is directed to stderr

**Scenario 2: Server completes MCP initialize handshake**
- **Given** the server is running via `atcr serve`
- **When** the client sends an `initialize` request with supported protocol version
- **Then** the server responds with `serverInfo` containing name `atcr` and version
- **And** the server advertises `tools` capability with the list of registered tool names

**Scenario 3: InMemoryTransport enables in-process testing**
- **Given** a test creates an MCP server with `InMemoryTransport`
- **When** the test sends tool calls through the in-memory transport
- **Then** the server processes requests without requiring stdin/stdout
- **And** no output leaks to stdout during test execution

## Edge Cases

**Edge Case 1: Stdin closed before initialize**
- **Given** `atcr serve` is started but stdin is immediately closed
- **When** the server attempts to read the first request
- **Then** the server logs a diagnostic to stderr and exits with code 0 (clean shutdown)

**Edge Case 2: Unsupported protocol version from client**
- **Given** the server is running via `atcr serve`
- **When** the client sends `initialize` with an unsupported protocol version
- **Then** the server responds with a JSON-RPC error and remains responsive
- **And** responsiveness is verified by a subsequent valid initialize/request succeeding in the same test

**Edge Case 3: Client closes stdin during operation**
- **Given** the server is running via `atcr serve` and the client closes stdin
- **When** in-flight requests complete
- **Then** the server exits 0 cleanly
- **And** no reconnect handling is needed: a stdio MCP server has exactly one client for its process lifetime (clients reconnect by spawning a new process)

**Edge Case 4: Malformed JSON-RPC payload**
- **Given** the client sends a malformed JSON-RPC payload (invalid JSON or missing `jsonrpc` field)
- **When** the server reads it
- **Then** the server responds with a protocol-level error (or ignores per JSON-RPC spec for unparseable messages) and does not crash
- **And** subsequent valid requests still succeed

## Error Conditions

**Error Scenario 1: Stdin is not a pipe (interactive terminal)**
- Error message: "atcr serve requires stdin/stdout pipe; use atcr review for interactive mode"
- Exit code: 1

**Error Scenario 2: Stdout write fails (broken pipe)**
- Error message (to stderr): "stdout write failed: broken pipe — MCP client disconnected"
- Exit code: 1

## Performance Requirements
- **Startup Time:** Server accepts first request within 50ms of process start
- **Handshake Latency:** MCP initialize round-trip completes in < 10ms (local process)
- **Throughput:** Server handles sequential tool calls with < 5ms overhead per call (excluding tool execution time)

## Security Considerations
- **Input Validation:** Server validates all incoming JSON-RPC requests conform to MCP schema before dispatching
- **No code execution:** Server does not eval or exec any client-provided code; tools only invoke internal packages
- **Stdout integrity:** Stdout is exclusively owned by MCP protocol; no log, debug, or human-readable output leaks to stdout

## Test Implementation Guidance
**Test Type:** UNIT + INTEGRATION
**Test Data Requirements:**
- Valid MCP initialize request JSON
- Tool call request payloads for each registered tool
**Mock/Stub Requirements:**
- Use `InMemoryTransport` for all tests (no stdio needed)
- Mock internal packages (fanout, reconcile) to verify handler dispatch without real LLM calls

**Test Cases:**
1. `TestServe_InitializeHandshake` — verify server responds to initialize with correct capabilities
2. `TestServe_StderrOnlyLogging` — verify no output to stdout except protocol messages
3. `TestServe_InMemoryTransport` — verify server works with in-memory transport in tests
4. `TestServe_StdinClosed` — verify clean exit when stdin closes
5. `TestServe_UnsupportedVersion` — verify graceful handling of version mismatch

## Definition of Done
**Auto-Verified:**
- [x] All tests passing (unit + integration)
- [x] No linting errors (`golangci-lint run`)
- [x] Build succeeds (`go build ./cmd/atcr`)
- [x] `atcr serve` starts and completes MCP initialize handshake

**Story-Specific:**
- [x] Stdio transport is the only transport configured (no HTTP/SSE)
- [x] All log/human output goes to stderr; stdout reserved for MCP protocol
- [x] InMemoryTransport available and used in tests
- [x] Server startup completes within 50ms

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Stderr discipline verified by manual inspection of serve.go

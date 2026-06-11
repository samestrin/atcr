# Acceptance Criteria: Tool Registration and Typed Schemas

**Related User Story:** [04: MCP Integration](../user-stories/04-mcp-integration.md)

## Implementation Technology
| Component | Technology | Notes |
|-----------|------------|-------|
| MCP SDK | `modelcontextprotocol/go-sdk` v1.6.1 | `mcp.AddTool` with generics for schema inference |
| Schema Generation | Go generics + JSON Schema | Typed args structs produce input schema automatically |
| Result Types | Go structs | Typed result structs produce output schema automatically |
| Test Framework | `testify` (assert, require) | Verify schema correctness and completeness |

## Related Files
- `internal/mcp/server.go` - create: Tool registration using `mcp.AddTool` for all five tools
- `internal/mcp/tools.go` - create: Typed args and result struct definitions for all tools
- `internal/mcp/tools_test.go` - create: Unit tests verifying schema generation and type correctness
- `internal/mcp/server.go` - modify: Register tools during server initialization

## Documentation References

This AC is implemented against the following project documentation. Read before implementation:

- [MCP Server Implementation](../documentation/mcp-server.md) — Authoritative spec for the generic `mcp.AddTool` pattern (typed `args` and result structs, `jsonschema` struct tags), the 5-tool table, and the `ReviewArgs`/`ReviewResult` example code.

### Spec alignment notes

- **The 5 tools are exactly**: `atcr_review`, `atcr_reconcile`, `atcr_report`, `atcr_range`, `atcr_status`. Tool names are part of the public contract — do not rename without a coordinated v2 bump.
- **Generic `mcp.AddTool` is the recommended pattern in v1.6.1**. Manual `Server.AddTool` is reserved for untyped/raw cases. Schema inference reads `jsonschema:"..."` struct tags.
- **Per-tool result shape** (per `mcp-server.md`):
  - `atcr_review` → `{review_dir, partial, findings}` (plus internal `agent_count` from engine output)
  - `atcr_reconcile` → reconciliation summary with `pass` field (true/false based on `--fail-on` threshold)
  - `atcr_report` → rendered content (markdown, JSON, or checklist)
  - `atcr_range` → `{base, head, commit_count, file_count}`
  - `atcr_status` → `{review_id, status, agent_count, agents_done, agents_pending}`
- **All tool args are optional** by design (clients call with `{}` to use defaults). The handler applies defaults (e.g., head=HEAD, base from git auto-detect).
- **Schema size budget**: each tool's input schema is < 2KB JSON per the AC perf target. Avoid large enum lists in tags; reference constants instead.

## Happy Path Scenarios

**Scenario 1: All five tools are registered on server startup**
- **Given** the MCP server is constructed via `NewServer()`
- **When** the server completes initialization
- **Then** exactly five tools are registered: `atcr_review`, `atcr_reconcile`, `atcr_report`, `atcr_range`, `atcr_status`
- **And** each tool has a non-empty description string

**Scenario 2: Tool schemas are inferred from Go types**
- **Given** `atcr_review` args struct defines fields `ID`, `Base`, `Head`, `MergeCommit` as `string` with `json` tags
- **When** the MCP SDK generates the input schema for `atcr_review`
- **Then** the schema contains properties matching the Go struct fields
- **And** required/optional markers match struct tag annotations

**Scenario 3: Each tool has typed result output**
- **Given** a tool handler returns a result struct (e.g., `ReviewResult`)
- **When** the MCP client calls the tool
- **Then** the result is serialized as JSON matching the tool's output schema
- **And** all fields are populated with correct types (no `interface{}` or raw maps)

## Edge Cases

**Edge Case 1: Tool name collision during registration**
- **Given** a developer accidentally registers two tools with the same name
- **When** `mcp.AddTool` is called for the second tool
- **Then** the second registration panics or returns an error (fail-fast)
- **And** the server does not start with ambiguous tool names

**Edge Case 2: Optional vs required fields in args schema**
- **Given** `atcr_review` args has all optional fields (ID, Base, Head, MergeCommit)
- **When** the client calls `atcr_review` with an empty args object `{}`
- **Then** the server accepts the call without validation errors
- **And** defaults are applied by the handler (e.g., head=HEAD, base resolved from git)

**Edge Case 3: Unknown tool called by client**
- **Given** the server has five registered tools
- **When** the client calls a tool named `atcr_unknown`
- **Then** the server returns a standard MCP error: "tool not found: atcr_unknown"
- **And** does not crash or hang

## Error Conditions

**Error Scenario 1: Schema generation fails for a tool**
- Error message: "failed to register tool atcr_review: schema generation error: <details>"
- Behavior: Server fails to start (fatal); this is a build-time issue, not runtime

**Error Scenario 2: Client sends malformed args JSON**
- Error message: "invalid args for atcr_review: json: cannot unmarshal number into Go struct field ..."
- MCP error code: -32602 (Invalid params)

## Performance Requirements
- **Schema Generation:** All five tool schemas generated at startup in < 10ms total
- **Schema Size:** Each tool's input schema is < 2KB JSON (minimal overhead for MCP client)

## Security Considerations
- **Input Validation:** MCP SDK validates incoming args against generated JSON Schema before handler dispatch
- **No arbitrary fields:** Extra fields in client args are rejected by strict schema validation
- **Type safety:** Go generics ensure handlers receive correctly typed args; no runtime type assertions needed

## Test Implementation Guidance
**Test Type:** UNIT
**Test Data Requirements:**
- Sample args structs for each tool
- Expected JSON Schema output for comparison
**Mock/Stub Requirements:**
- None — test schema generation directly from Go types

**Test Cases:**
1. `TestToolRegistration_Count` — verify exactly 5 tools registered
2. `TestToolRegistration_Names` — verify exact tool names match spec
3. `TestToolSchema_ReviewArgs` — verify atcr_review schema has correct fields and types
4. `TestToolSchema_ReconcileArgs` — verify atcr_reconcile schema
5. `TestToolSchema_ReportArgs` — verify atcr_report schema with format enum
6. `TestToolSchema_AllTools` — table-driven: verify all 5 tools have non-empty descriptions
7. `TestToolCall_UnknownTool` — verify error on unknown tool name

## Definition of Done
**Auto-Verified:**
- [ ] All tests passing (unit)
- [ ] No linting errors (`golangci-lint run`)
- [ ] Build succeeds (`go build ./cmd/atcr`)
- [ ] `tools/list` MCP call returns exactly 5 tools with valid schemas

**Story-Specific:**
- [ ] Tools registered: atcr_review, atcr_reconcile, atcr_report, atcr_range, atcr_status
- [ ] Each tool uses `mcp.AddTool` with generic typed args and result structs
- [ ] All args fields are optional (client can call with `{}`)
- [ ] atcr_report `format` field accepts enum: `md`, `json`, `checklist`
- [ ] Tool descriptions are clear and actionable for MCP clients

**Manual Review:**
- [ ] Code reviewed and approved
- [ ] Tool descriptions reviewed for clarity and accuracy

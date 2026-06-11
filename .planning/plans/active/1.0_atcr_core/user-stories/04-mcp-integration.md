# User Story 4: MCP Integration

**Plan:** [1.0: atcr Core - Review Engine, Reconciler, and Skill](../plan.md)

## User Story

**As an** IDE or AI agent (MCP client)
**I want** to invoke atcr review, reconcile, and report via MCP protocol
**So that** I can integrate multi-agent code review into my workflow without shelling out to CLI

## Story Context

- **Background:** MCP (Model Context Protocol) enables AI agents and IDEs to call tools via a standardized protocol. atcr exposes its engine as MCP tools, allowing clients like Claude Code, Cursor, or custom agents to trigger reviews programmatically.
- **Assumptions:** MCP client supports stdio transport. Client can pass typed arguments and parse structured results. atcr binary is available in PATH or configured as MCP server.
- **Constraints:** Stdio transport only in v1 (no HTTP/SSE). stdout owned by MCP protocol; all human/log output to stderr. No logic in handlers — thin layer over internal packages.

## Story Details

| Field | Value |
|-------|-------|
| **Priority** | Medium |
| **Effort Estimate** | M |
| **Dependencies** | CLI Review Workflow (US-01), Reconciler |

## Success Criteria (SMART Format)

- **Specific:** MCP client can connect to `atcr serve` via stdio, call atcr_review/atcr_reconcile/atcr_report/atcr_range/atcr_status tools, and receive typed results
- **Measurable:** Each tool returns structured result (JSON) matching schema; no errors on valid input
- **Achievable:** Uses modelcontextprotocol/go-sdk with generic mcp.AddTool and typed args/result
- **Relevant:** Enables IDE/agent integration — expands atcr's reach beyond CLI users
- **Time-bound:** Implemented in task 11 (MCP server)

## Acceptance Criteria

| AC | Title | Type |
|----|-------|------|
| [04-01](../acceptance-criteria/04-01-mcp-stdio-server.md) | MCP Stdio Server Startup | Unit/Integration |
| [04-02](../acceptance-criteria/04-02-tool-registration-schemas.md) | Tool Registration and Typed Schemas | Unit |
| [04-03](../acceptance-criteria/04-03-review-reconcile-handlers.md) | Review and Reconcile Tool Handlers | Unit/Integration |
| [04-04](../acceptance-criteria/04-04-report-range-status-handlers.md) | Report, Range, and Status Tool Handlers | Unit/Integration |

## Original Criteria Overview

1. `atcr serve` starts MCP stdio server
2. Five tools registered: atcr_review, atcr_reconcile, atcr_report, atcr_range, atcr_status
3. Each tool has typed args (input schema) and typed result (output schema)
4. atcr_review args: id (optional), base (optional), head (optional), merge_commit (optional)
5. atcr_reconcile args: id_or_path (optional, defaults to .atcr/latest), fail_on (optional)
6. atcr_report args: id_or_path (optional), format (md|json|checklist)
7. atcr_range args: base (optional), head (optional), merge_commit (optional)
8. atcr_status args: id_or_path (optional)
9. Handlers call same internal packages as CLI (no logic duplication)
10. stdout reserved for MCP protocol; all logs/errors to stderr
11. InMemoryTransport available for testing

## Technical Considerations

- **Implementation Notes:** 
  - MCP server: internal/mcp/server.go — uses modelcontextprotocol/go-sdk
  - Tool registration: generic mcp.AddTool with typed args/result for schema inference
  - Transport: StdioTransport (only v1 transport)
  - Handlers: thin wrappers over cmd/atcr/* logic; no business logic in handlers
  - Stderr discipline: in serve mode, all human/log output to stderr (stdout owned by protocol)
  - Testing: InMemoryTransport for integration tests (no stdio needed)

- **Integration Points:** 
  - MCP clients: Claude Code, Cursor, custom agents
  - Internal packages: fanout, reconcile, report, gitrange
  - Filesystem: review directory, .atcr/latest pointer

- **Data Requirements:** 
  - Tool schemas: JSON Schema inferred from Go types
  - Results: structured JSON (e.g., atcr_review returns {review_id, review_path, partial, agent_count})
  - atcr_report returns rendered content (markdown, JSON, or checklist)

## Potential Risks

| Risk | Impact | Mitigation |
|------|--------|------------|
| Stdout pollution breaks MCP protocol | High | Strict stderr discipline; test with InMemoryTransport |
| Handler logic diverges from CLI | Medium | Thinhandlers call same internal functions; no duplication |
| Tool schema changes break clients | Medium | Version tools (v1 suffix) or document additive-only evolution |
| Long-running review blocks MCP client | Medium | atcr_review returns immediately with review_id; client polls atcr_status |
| MCP SDK version incompatibility | Low | Pin mcp-go-sdk version in go.mod (v1.6.1 per registry.yaml) |

---

**Created:** June 10, 2026
**Status:** Draft - Awaiting Acceptance Criteria

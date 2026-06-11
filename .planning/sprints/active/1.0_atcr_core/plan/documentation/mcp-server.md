# MCP Server Implementation <span style="color: #ff6b6b;">[CRITICAL]</span>

## Overview

The atcr MCP (Model Context Protocol) server exposes the review engine as callable tools through the stdio transport, enabling IDEs, agents, and other MCP clients to integrate code review capabilities without spawning CLI subprocesses. The server is implemented as a thin layer over the shared internal engine—no logic exists in the handlers themselves, ensuring identical behavior between CLI and MCP modes.

The MCP server is started via `atcr serve` and communicates exclusively through stdin/stdout using the Model Context Protocol. All five core tools (`atcr_review`, `atcr_reconcile`, `atcr_report`, `atcr_range`, `atcr_status`) are exposed with typed argument schemas that enable automatic discovery and validation by MCP clients.

> Source: [plan.md:MCP server — atcr serve with 5 tool handlers], [.planning/specifications/packages/mcp-go-sdk.md]

## Key Concepts

### Stdio Transport Ownership

The stdio transport owns stdout when `atcr serve` is running. This means all human-readable output, logs, and diagnostic messages must go to stderr—stdout is reserved exclusively for MCP protocol messages. Violating this boundary causes protocol corruption and client disconnection.

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Integration Notes (atcr)], [plan.md:Filesystem Discipline]

### Generic Tool Registration

Tools are registered using the generic `mcp.AddTool` function from the modelcontextprotocol/go-sdk with typed argument/result structs. This approach enables automatic schema inference—the SDK derives the JSON Schema for tool arguments from Go struct field tags, eliminating manual schema construction and keeping the handler code minimal.

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Adding tools — generic `AddTool` (recommended)]

### Shared Engine Architecture

The MCP server wraps the same internal packages used by the CLI commands: `gitrange`, `payload`, `registry`, `llmclient`, `fanout`, `stream`, `reconcile`, `report`. Handler functions extract arguments, construct engine input structs, invoke the appropriate method, and return results. No business logic lives in handlers—this ensures bug fixes and feature additions apply uniformly across both interfaces.

> Source: [plan.md:Architecture — Single Go binary with internal packages]

### Context Propagation

All handler methods receive and honor the Go `context.Context` passed by the MCP SDK. This enables timeout propagation from the MCP client—if a client sets a 10-minute deadline, the engine respects it, canceling fan-out operations and LLM calls cleanly. Handlers must check context cancellation before long-running operations and return early errors rather than continuing work.

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Common Patterns — honor `ctx` everywhere]

### InMemoryTransport for Testing

The MCP server implementation supports an `InMemoryTransport` alternative to stdio for unit testing. Tests instantiate the server with in-memory pipes, call tools programmatically, and assert on result structures without spawning external processes. This pattern keeps MCP integration tests fast and deterministic.

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Core API — Transports]

## Code Examples

### Typed Tool Handler (generic `mcp.AddTool`)

The package-level generic `mcp.AddTool` infers the input schema from the typed `args` parameter and the output schema from the typed result, reading `jsonschema` struct tags. This is the recommended pattern in v1.6.1 — manual `Server.AddTool` is reserved for untyped/raw cases.

```go
type ReviewArgs struct {
    ID   string `json:"id,omitempty" jsonschema:"review id; defaults to a generated id"`
    Base string `json:"base,omitempty" jsonschema:"base ref"`
    Head string `json:"head,omitempty" jsonschema:"head ref"`
}

type ReviewResult struct {
    ReviewDir string `json:"review_dir"`
    Partial   bool   `json:"partial"`
    Findings  int    `json:"findings"`
}

mcp.AddTool(server, &mcp.Tool{
    Name:        "atcr_review",
    Description: "Fan a git range out to the reviewer pool",
}, func(ctx context.Context, req *mcp.CallToolRequest, in ReviewArgs) (*mcp.CallToolResult, ReviewResult, error) {
    res, err := engine.Review(ctx, in.ID, in.Base, in.Head)
    if err != nil {
        return nil, ReviewResult{}, err
    }
    return nil, ReviewResult{ReviewDir: res.Dir, Partial: res.Partial, Findings: res.Count}, nil
})
```

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Adding tools — generic `AddTool` (recommended)]

### Server Initialization and Registration

```go
server := mcp.NewServer(&mcp.Implementation{Name: "atcr", Version: "1.0.0"}, nil)

mcp.AddTool(server, &mcp.Tool{Name: "atcr_review",    Description: "Fan a git range out to the reviewer pool"},    handleReview)
mcp.AddTool(server, &mcp.Tool{Name: "atcr_reconcile",  Description: "Merge multi-source findings with confidence"}, handleReconcile)
mcp.AddTool(server, &mcp.Tool{Name: "atcr_report",     Description: "Render views over reconciled findings"},       handleReport)
mcp.AddTool(server, &mcp.Tool{Name: "atcr_range",      Description: "Pre-flight range resolution only"},             handleRange)
mcp.AddTool(server, &mcp.Tool{Name: "atcr_status",     Description: "Query current review state"},                   handleStatus)
```

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Creating and running a server over stdio]

### Stdio Server Entry Point

`Run` blocks serving the session; context cancellation shuts the server down. `StdioTransport` is the only v1 transport for `atcr serve`.

```go
func ServeCmd(ctx context.Context) error {
    engine, err := NewEngine()
    if err != nil {
        return fmt.Errorf("create engine: %w", err)
    }

    server := mcp.NewServer(&mcp.Implementation{Name: "atcr", Version: "1.0.0"}, nil)
    // ... register tools against `server` ...

    if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
        return fmt.Errorf("serve stdio: %w", err)
    }
    return nil
}
```

> Source: [.planning/specifications/packages/mcp-go-sdk.md:Creating and running a server over stdio]

## Quick Reference

| Tool | Purpose | Key Arguments | Output |
|------|---------|---------------|--------|
| `atcr_review` | Resolve range, build payloads, fan out to persona pool | `--base`, `--head`, `--merge-commit`, `--id` | Review directory path, per-agent artifacts |
| `atcr_reconcile` | Normalize, cluster, dedupe, compute confidence | `--id-or-path`, `--fail-on` | Reconciled findings.txt/json, report.md |
| `atcr_report` | Render views over reconciled findings | `--id-or-path`, `--format` | Markdown, JSON, or checklist output |
| `atcr_range` | Pre-flight range resolution only | `--base`, `--head`, `--merge-commit` | Resolution JSON with base/head SHAs |
| `atcr_status` | Query current review state | `--id-or-path` | Manifest.json contents |

## Related Documentation

- [Plan Document](../plan.md) — Full implementation plan with task breakdown
- [Original Requirements](../original-requirements.md) — Epic requirements with MCP server specification
- [Package Recommendations](../package-recommendations.md) — MCP SDK dependency details
- [MCP Go SDK Spec](../../../../specifications/packages/mcp-go-sdk.md) — Authoritative SDK API reference (v1.6.1)
- [Coding Standards](../../../../specifications/coding-standards.md) — Go conventions (error wrapping, context propagation)

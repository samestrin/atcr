# github.com/modelcontextprotocol/go-sdk

**Version:** v1.6.1 (May 22, 2026)
**Registry:** [pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp)
**Official Docs:** [github.com/modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk)
**Tier:** Critical
**Last Updated:** June 10, 2026

---

## Overview

The official Go SDK for the Model Context Protocol. The `mcp` package builds MCP servers and clients communicating over stdio, in-memory, or HTTP/SSE transports, exposing tools, resources, and prompts. Licenses: Apache-2.0, CC-BY-4.0, MIT. Note: this SDK evolved quickly through 2025–2026; pin the version in go.mod and re-check the changelog before upgrading.

## Installation

```bash
go get github.com/modelcontextprotocol/go-sdk@latest
```

```go
import "github.com/modelcontextprotocol/go-sdk/mcp"
```

## Core API

### Creating and running a server over stdio

```go
server := mcp.NewServer(&mcp.Implementation{Name: "atcr", Version: "1.0.0"}, nil)

if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
    log.Fatal(err)
}
```

`Run` blocks serving the session; context cancellation shuts it down.

### Adding tools — generic `AddTool` (recommended)

The package-level generic `mcp.AddTool` infers the input schema from the typed `args` parameter (and the output schema from the typed output, when not `any`), reads property descriptions from `jsonschema` struct tags, and validates inputs before the handler runs:

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
    if err != nil { return nil, ReviewResult{}, err }
    return nil, ReviewResult{ReviewDir: res.Dir, Partial: res.Partial, Findings: res.Count}, nil
})
```

Input/output types must be structs or maps (object schemas). Returning a typed output value yields structured content automatically.

### Manual `Server.AddTool`

For raw/untyped handling: `server.AddTool(&mcp.Tool{Name: "x"}, handler)` — schema and validation are the caller's responsibility. Prefer the generic form.

### Content types

Tool results carry `[]mcp.Content`: `TextContent`, `ImageContent`, `AudioContent`, `EmbeddedResource`. For atcr, structured output (typed result) plus a short `TextContent` summary is the right shape.

### Other server capabilities

- `AddResource` / `AddResourceTemplate`, `AddPrompt` — not needed for atcr v1.
- `ServerSession.NotifyProgress` — progress reporting for long fan-outs (nice-to-have).
- `AddReceivingMiddleware` / `AddSendingMiddleware` — request/response interceptors.
- Transports: `StdioTransport` (atcr's only v1 transport), `CommandTransport`, `InMemoryTransport` (useful in tests), `SSEServerTransport`, `StreamableServerTransport`.

## Common Patterns

- One engine, thin handlers: each MCP tool handler parses typed args and calls the same internal package the CLI command calls. No logic in handlers (Epic 1.0 constraint).
- Use `InMemoryTransport` to integration-test `atcr serve` without spawning a subprocess.
- Honor `ctx` everywhere — the SDK propagates client cancellation into handlers, which must flow into the fan-out engine's global-timeout context.

## Integration Notes (atcr)

- `atcr serve` (Epic 1.0 task 11) exposes `atcr_review`, `atcr_reconcile`, `atcr_report`, `atcr_range`, `atcr_status` over `StdioTransport`.
- Long-running reviews: an MCP tool call holds the connection; either rely on client timeouts being generous, or return early with the review dir and expose `atcr_status` for polling.
- stdout discipline: stdio transport owns stdout — all human/log output inside `serve` mode must go to stderr.

---
**Source:** Extracted from pkg.go.dev on June 10, 2026.

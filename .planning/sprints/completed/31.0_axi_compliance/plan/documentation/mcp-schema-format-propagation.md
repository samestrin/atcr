# MCP Tool Schema & Format-Enum Propagation

**Priority:** Important

## Overview

`jsonschema-go` is the reflection engine at the bottom of this stack: it derives a JSON Schema from a Go struct type by walking its fields and reading `json` / `jsonschema` struct tags — `Reflector.Reflect(v)` returns a `*jsonschema.Schema` for the type of `v`, using `json:"name,omitempty"` to control property naming/optionality and `jsonschema:"description text"` to set the property description (jsonschema-go.md:Core API). atcr already depends on this package directly: `internal/mcp/tools.go` uses `jsonschema.Reflector{}` with default settings to build the `atcr_report` input schema and other MCP tool argument schemas (jsonschema-go.md:Integration Notes).

The MCP Go SDK builds directly on top of that reflection engine rather than reimplementing schema generation. Its package-level generic `mcp.AddTool[In, Out](s *Server, t *Tool, h ToolHandlerFor[In, Out])` infers the input schema from the typed `args` parameter (and the output schema from the typed output, when not `any`), reads property descriptions from `jsonschema` struct tags, and validates inputs before the handler runs (mcp-go-sdk.md:Adding tools — generic AddTool). The SDK's own `Tool` struct carries this generated result directly: `InputSchema *jsonschema.Schema` and `OutputSchema *jsonschema.Schema` are typed as `jsonschema-go` schema values (mcp-sdk.md:Tool Types), confirming the SDK's tool-registration layer is a thin typed wrapper around the jsonschema-go reflection call.

For atcr's own `atcr_report` tool, this chain means the *enum* of allowed format values is not manually authored anywhere in the MCP layer — it is derived data. Per the codebase-discovery evidence, `internal/mcp/tools.go`'s `reportInputSchema`/`descReport` (line 224) build the format enum from `report.FormatList()` (lines 234–238) and the tool description from `report.Formats()` (line 216); `internal/mcp/handlers.go`'s `handleReport` (line 378) then re-validates the format via `report.ValidFormat()` before dispatch (codebase-discovery.json). Because the enum and description are computed from the same `internal/report` format-registration source that also drives the CLI's `--format` flag, any new format constant added to that package's enum pattern surfaces in the MCP tool schema automatically, without a corresponding MCP-layer code change — the same mechanism that made CLI/MCP parity an explicit, deliberately-tracked acceptance criterion for Sprint 25.0 (SARIF) after TD-003 caught the enum drifting (codebase-discovery.json architecture_note). This document describes the propagation mechanism itself, as background for reasoning about the axi format's exposure — the decision of whether/how to expose `--axi` through MCP is owned by plan.md and the Technical Planning Notes, not by this document.

## Key Concepts

### Reflection-based schema generation (Go struct → JSON Schema)

`jsonschema-go` generates a schema from a Go type via `Reflector.Reflect(v)`, driven entirely by struct tags rather than hand-written schema documents:

```go
type ReviewArgs struct {
    ID   string `json:"id,omitempty"   jsonschema:"review id; defaults to a generated id"`
    Base string `json:"base,omitempty" jsonschema:"base git ref"`
    Head string `json:"head,omitempty" jsonschema:"head git ref"`
}

reflector := jsonschema.Reflector{}
schema, err := reflector.Reflect(ReviewArgs{})
```

> Source: [jsonschema-go.md: Core API]

The struct-tag contract is fixed: `json:"name"` sets the property name, `json:"name,omitempty"` makes the property optional (excluded from `required`), and `jsonschema:"desc"` sets the property's `description` (jsonschema-go.md: Struct Tags table).

### The reflection path is separate from schema *validation*

jsonschema-go is bidirectional. The reflection direction above (Go struct → schema) is distinct from its other direction: parsing an externally-authored JSON Schema document (`Schema.UnmarshalJSON`), resolving its `$ref`s (`Schema.Resolve`), and validating an arbitrary decoded JSON value against it (`Resolved.Validate`). atcr does not yet exercise the validation direction anywhere in the repo as of this writing; it is documented for context on the package's full surface, not because the AXI/MCP propagation mechanism depends on it.

> Source: [jsonschema-go.md: Validating JSON Against an External Schema]

### Generic `AddTool[In, Out]` is the SDK's schema-inference entry point

The MCP Go SDK's registration API has two forms: a low-level `Server.AddTool(t *Tool, h ToolHandler)` where "schema and validation are the caller's responsibility," and the generic `mcp.AddTool[In, Out]` which automatically generates JSON schemas from the Go input/output types, validates inputs, and deserializes arguments before the handler runs. The SDK's own guidance is to "use the generic form wherever possible — it eliminates manual schema authoring and validation."

> Source: [mcp-go-sdk.md: Adding tools — generic AddTool (recommended)]
> Source: [mcp-sdk.md: Integration Notes → Tool Handler Signatures]

### Tool schema fields are typed as `jsonschema.Schema`

The SDK's wire-level `Tool` struct declares `InputSchema *jsonschema.Schema` and `OutputSchema *jsonschema.Schema` — i.e., the schema fields sent to MCP clients are literally jsonschema-go values, not an SDK-private schema representation. This is the structural link that makes jsonschema-go's reflection output the same object the MCP client receives when it lists tools.

> Source: [mcp-sdk.md: Tool Types]

### atcr's own usage of the mechanism

atcr's MCP server layer (`internal/mcp/tools.go`) uses `jsonschema.Reflector{}` with default settings (no `AllowAdditionalProperties` override) to build tool input schemas, and relies on the SDK's `mcp.AddTool` generic helper to derive schemas for MCP tool arguments automatically.

> Source: [jsonschema-go.md: Integration Notes (atcr)]

The auto-propagation described above is pinned by test, not just by construction: `internal/mcp/tools_test.go:108-130` derives the expected schema enum from `report.FormatList()` and the expected description from `report.Formats()` and asserts the generated `atcr_report` schema matches both ("so a future format add/remove cannot drift out of sync with the schema enum and report.ValidFormat (AC 04-04)"). A new format constant therefore surfaces to MCP clients under CI enforcement — enum/description drift fails the build, which is why the axi parity stance must be decided deliberately rather than discovered after the fact (codebase-discovery.json; Sprint 25.0 AC 04-04).

### Schema tags carry field descriptions end-to-end

The `jsonschema` struct tag supplies the per-field description that ends up in the generated schema's `description` property, demonstrated identically in both the jsonschema-go and MCP SDK docs:

```go
type Input struct {
    Name string `json:"name" jsonschema:"the name of the person"`
    Age  int    `json:"age,omitempty" jsonschema:"the person's age in years"`
}
```

> Source: [mcp-sdk.md: Integration Notes → Schema Tags]

## Code Examples

The following are verbatim from the source documentation.

### jsonschema-go: `Reflector.Reflect` (struct → schema)

```go
import "github.com/google/jsonschema-go/jsonschema"

type ReviewArgs struct {
    ID   string `json:"id,omitempty"   jsonschema:"review id; defaults to a generated id"`
    Base string `json:"base,omitempty" jsonschema:"base git ref"`
    Head string `json:"head,omitempty" jsonschema:"head git ref"`
}

reflector := jsonschema.Reflector{}
schema, err := reflector.Reflect(ReviewArgs{})
```

> Source: [jsonschema-go.md: Core API]

### mcp-go-sdk: generic `mcp.AddTool` registration

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

> Source: [mcp-go-sdk.md: Adding tools — generic AddTool (recommended)]

### mcp-sdk: `Tool` struct and generic registration signature

```go
type Tool struct {
    Name         string              `json:"name"`
    Description  string              `json:"description,omitempty"`
    InputSchema  *jsonschema.Schema  `json:"inputSchema"`
    OutputSchema *jsonschema.Schema  `json:"outputSchema,omitempty"`
    Annotations  *ToolAnnotations    `json:"annotations,omitempty"`
    Icons        []*Icon             `json:"icons,omitempty"`
    Meta         Meta                `json:"_meta,omitempty"`
}

// Handler signatures
type ToolHandler = func(ctx context.Context, req *CallToolRequest) (*CallToolResult, error)
type ToolHandlerFor[In, Out any] = func(ctx context.Context, req *CallToolRequest, args In) (*CallToolResult, Out, error)

// Generic registration — infers input/output schemas from Go types
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])
```

```go
type Input struct {
    Name string `json:"name" jsonschema:"the name of the person to greet"`
}
```

> Source: [mcp-sdk.md: Tool Types]

### mcp-sdk: basic server with typed tool (full worked example)

```go
package main

import (
    "context"
    "log"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

type GreetInput struct {
    Name string `json:"name" jsonschema:"the name of the person to greet"`
}

type GreetOutput struct {
    Greeting string `json:"greeting" jsonschema:"the greeting message"`
}

func SayHi(ctx context.Context, req *mcp.CallToolRequest, input GreetInput) (
    *mcp.CallToolResult, GreetOutput, error,
) {
    return nil, GreetOutput{Greeting: "Hi " + input.Name}, nil
}

func main() {
    server := mcp.NewServer(
        &mcp.Implementation{Name: "greeter", Version: "v1.0.0"}, nil,
    )
    mcp.AddTool(server, &mcp.Tool{
        Name: "greet", Description: "say hi",
    }, SayHi)

    if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
        log.Fatal(err)
    }
}
```

> Source: [mcp-sdk.md: Common Patterns → Basic Server with Typed Tool]

## Quick Reference

| Package | Role in the propagation chain | Key API surfaced in sources |
|---|---|---|
| `jsonschema-go` | Reflection/validation engine: converts a Go struct type into a `*jsonschema.Schema` via `json`/`jsonschema` struct tags; also parses and validates against externally-authored schemas | `Reflector.Reflect(v)`, `Schema.UnmarshalJSON`, `Schema.Resolve`, `Resolved.Validate` |
| `mcp-go-sdk` (go-sdk, package `mcp`) | MCP server/tool registration built on top of jsonschema-go; the generic `AddTool[In, Out]` infers schemas from typed args/results instead of hand-authoring them | `mcp.NewServer`, `mcp.AddTool[In, Out]`, `Server.AddTool` (low-level, manual schema) |
| `mcp-sdk` (same module, full API surface) | Full SDK surface: `Tool` struct's `InputSchema`/`OutputSchema` fields are typed as `*jsonschema.Schema`, confirming the schema object handed to MCP clients is jsonschema-go's reflection output; also documents `jsonrpc`, `auth`, `oauthex` packages | `Tool{InputSchema, OutputSchema}`, `ToolHandlerFor[In, Out]`, `AddTool[In, Out]` |
| `internal/mcp/tools.go` (atcr) | Consumes jsonschema-go directly (`jsonschema.Reflector{}`, default settings) to build the `atcr_report` input schema; derives the format enum from `report.FormatList()` and description from `report.Formats()` | per codebase-discovery.json |
| `internal/mcp/handlers.go` (atcr) | Defensively re-validates the format via `report.ValidFormat()` before dispatch — a new enum constant passes MCP validation automatically, but exposure is a deliberate decision, not an automatic given | per codebase-discovery.json |

## Related Documentation

- [../plan.md](../plan.md)
- [jsonschema-go.md](/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/packages/jsonschema-go.md)
- [mcp-go-sdk.md](/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/packages/mcp-go-sdk.md)
- [mcp-sdk.md](/Users/samestrin/Documents/GitHub/atcr/.planning/specifications/packages/mcp-sdk.md)

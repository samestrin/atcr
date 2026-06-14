# mcp-sdk

**Version:** v1.6.1
**Registry:** [pkg.go.dev/github.com/modelcontextprotocol/go-sdk](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk)
**Official Docs:** [https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk)
**Tier:** Critical
**Last Updated:** June 14, 2026

---

## Overview

The official Go SDK for the [Model Context Protocol (MCP)](https://modelcontextprotocol.io), maintained under the `modelcontextprotocol` GitHub organization. It provides a type-safe, idiomatic Go SDK for building MCP clients and servers, implementing the full MCP specification.

The SDK is organized into four importable packages:

| Package | Purpose |
|---------|---------|
| `mcp` | Primary API for constructing and using MCP clients and servers |
| `jsonrpc` | JSON-RPC v2 implementation for custom transport authors |
| `auth` | OAuth primitives for server-side bearer token verification and client-side authorization code flow |
| `oauthex` | OAuth extensions including Protected Resource Metadata (RFC 9728) |

### MCP Spec Version Compatibility

| SDK Version | Latest MCP Spec | All Supported Specs |
|---|---|---|
| v1.4.0+ | 2025-11-25* | 2025-11-25*, 2025-06-18, 2025-03-26, 2024-11-05 |
| v1.2.0 – v1.3.1 | 2025-11-25** | 2025-11-25**, 2025-06-18, 2025-03-26, 2024-11-05 |
| v1.0.0 – v1.1.0 | 2025-06-18 | 2025-06-18, 2025-03-26, 2024-11-05 |

\* Client-side OAuth has experimental support.
\*\* Partial support (client-side OAuth and Sampling with tools unavailable).

---

## Installation

```bash
go get github.com/modelcontextprotocol/go-sdk
```

Import paths:

```go
import (
    "github.com/modelcontextprotocol/go-sdk/mcp"       // core API
    "github.com/modelcontextprotocol/go-sdk/jsonrpc"    // custom transports
    "github.com/modelcontextprotocol/go-sdk/auth"       // OAuth
    "github.com/modelcontextprotocol/go-sdk/oauthex"    // OAuth extensions
)
```

---

## Core API

### Server

```go
type Server struct { /* unexported */ }

func NewServer(impl *Implementation, options *ServerOptions) *Server

// Registration
func (s *Server) AddTool(t *Tool, h ToolHandler)
func (s *Server) AddResource(r *Resource, h ResourceHandler)
func (s *Server) AddResourceTemplate(t *ResourceTemplate, h ResourceHandler)
func (s *Server) AddPrompt(p *Prompt, h PromptHandler)
func (s *Server) RemoveTools(names ...string)
func (s *Server) RemoveResources(uris ...string)
func (s *Server) RemoveResourceTemplates(uriTemplates ...string)
func (s *Server) RemovePrompts(names ...string)

// Middleware
func (s *Server) AddReceivingMiddleware(middleware ...Middleware)
func (s *Server) AddSendingMiddleware(middleware ...Middleware)

// Lifecycle
func (s *Server) Run(ctx context.Context, t Transport) error
func (s *Server) Connect(ctx context.Context, t Transport, opts *ServerSessionOptions) (*ServerSession, error)
func (s *Server) Sessions() iter.Seq[*ServerSession]
func (s *Server) ResourceUpdated(ctx context.Context, params *ResourceUpdatedNotificationParams) error
```

### Client

```go
type Client struct { /* unexported */ }

func NewClient(impl *Implementation, options *ClientOptions) *Client

func (c *Client) AddRoots(roots ...*Root)
func (c *Client) RemoveRoots(uris ...string)
func (c *Client) AddReceivingMiddleware(middleware ...Middleware)
func (c *Client) AddSendingMiddleware(middleware ...Middleware)
func (c *Client) Connect(ctx context.Context, t Transport, opts *ClientSessionOptions) (*ClientSession, error)
```

### Sessions

**ServerSession** — server-side view of a connected client:

```go
func (ss *ServerSession) ID() string
func (ss *ServerSession) InitializeParams() *InitializeParams
func (ss *ServerSession) Close() error
func (ss *ServerSession) Wait() error
func (ss *ServerSession) Ping(ctx context.Context, params *PingParams) error
func (ss *ServerSession) CreateMessage(ctx context.Context, params *CreateMessageParams) (*CreateMessageResult, error)
func (ss *ServerSession) CreateMessageWithTools(ctx context.Context, params *CreateMessageWithToolsParams) (*CreateMessageWithToolsResult, error)
func (ss *ServerSession) Elicit(ctx context.Context, params *ElicitParams) (*ElicitResult, error)
func (ss *ServerSession) ListRoots(ctx context.Context, params *ListRootsParams) (*ListRootsResult, error)
func (ss *ServerSession) Log(ctx context.Context, params *LoggingMessageParams) error
func (ss *ServerSession) NotifyProgress(ctx context.Context, params *ProgressNotificationParams) error
```

**ClientSession** — client-side view with full MCP method access:

```go
// Tools
func (cs *ClientSession) CallTool(ctx context.Context, params *CallToolParams) (*CallToolResult, error)
func (cs *ClientSession) ListTools(ctx context.Context, params *ListToolsParams) (*ListToolsResult, error)
func (cs *ClientSession) Tools(ctx context.Context, params *ListToolsParams) iter.Seq2[*Tool, error]

// Resources
func (cs *ClientSession) ReadResource(ctx context.Context, params *ReadResourceParams) (*ReadResourceResult, error)
func (cs *ClientSession) ListResources(ctx context.Context, params *ListResourcesParams) (*ListResourcesResult, error)
func (cs *ClientSession) Resources(ctx context.Context, params *ListResourcesParams) iter.Seq2[*Resource, error]
func (cs *ClientSession) ListResourceTemplates(ctx context.Context, params *ListResourceTemplatesParams) (*ListResourceTemplatesResult, error)
func (cs *ClientSession) ResourceTemplates(ctx context.Context, params *ListResourceTemplatesParams) iter.Seq2[*ResourceTemplate, error]
func (cs *ClientSession) Subscribe(ctx context.Context, params *SubscribeParams) error
func (cs *ClientSession) Unsubscribe(ctx context.Context, params *UnsubscribeParams) error

// Prompts
func (cs *ClientSession) GetPrompt(ctx context.Context, params *GetPromptParams) (*GetPromptResult, error)
func (cs *ClientSession) ListPrompts(ctx context.Context, params *ListPromptsParams) (*ListPromptsResult, error)
func (cs *ClientSession) Prompts(ctx context.Context, params *ListPromptsParams) iter.Seq2[*Prompt, error]

// Completion, Logging, Progress
func (cs *ClientSession) Complete(ctx context.Context, params *CompleteParams) (*CompleteResult, error)
func (cs *ClientSession) SetLoggingLevel(ctx context.Context, params *SetLoggingLevelParams) error
func (cs *ClientSession) NotifyProgress(ctx context.Context, params *ProgressNotificationParams) error

// Lifecycle
func (cs *ClientSession) ID() string
func (cs *ClientSession) InitializeResult() *InitializeResult
func (cs *ClientSession) Close() error
func (cs *ClientSession) Wait() error
func (cs *ClientSession) Ping(ctx context.Context, params *PingParams) error
```

### Tool Types

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

type ToolAnnotations struct {
    Title           string `json:"title,omitempty"`
    ReadOnlyHint    *bool  `json:"readOnlyHint,omitempty"`
    DestructiveHint *bool  `json:"destructiveHint,omitempty"`
    IdempotentHint  *bool  `json:"idempotentHint,omitempty"`
    OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
}

// Handler signatures
type ToolHandler = func(ctx context.Context, req *CallToolRequest) (*CallToolResult, error)
type ToolHandlerFor[In, Out any] = func(ctx context.Context, req *CallToolRequest, args In) (*CallToolResult, Out, error)

// Generic registration — infers input/output schemas from Go types
func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])
```

Input/output structs use `json` and `jsonschema` struct tags for schema generation:

```go
type Input struct {
    Name string `json:"name" jsonschema:"the name of the person to greet"`
}
```

### Content Types

```go
type TextContent struct {
    Text        string       `json:"text"`
    Meta        Meta         `json:"_meta,omitempty"`
    Annotations *Annotations `json:"annotations,omitempty"`
}

type ImageContent struct {
    Data     string `json:"data"`     // base64-encoded
    MIMEType string `json:"mimeType"`
}

type AudioContent struct {
    Data     []byte `json:"data"`
    MIMEType string `json:"mimeType"`
}

type EmbeddedResource struct {
    Resource *ResourceContents `json:"resource"`
}

type ResourceLink struct {
    Name        string `json:"name"`
    URI         string `json:"uri"`
    Description string `json:"description,omitempty"`
    MIMEType    string `json:"mimeType,omitempty"`
}
```

### Resource Types

```go
type Resource struct {
    URI         string `json:"uri"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    MIMEType    string `json:"mimeType,omitempty"`
}

type ResourceTemplate struct {
    URITemplate string `json:"uriTemplate"`
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    MIMEType    string `json:"mimeType,omitempty"`
}

type ResourceContents struct {
    URI      string `json:"uri"`
    MIMEType string `json:"mimeType,omitempty"`
    Text     string `json:"text,omitempty"`
    Blob     string `json:"blob,omitempty"` // base64 for binary
}

type ResourceHandler = func(ctx context.Context, req *ReadResourceRequest) (*ReadResourceResult, error)
```

### Prompt Types

```go
type Prompt struct {
    Name        string            `json:"name"`
    Description string            `json:"description,omitempty"`
    Arguments   []*PromptArgument `json:"arguments,omitempty"`
}

type PromptArgument struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    Required    bool   `json:"required,omitempty"`
}

type PromptMessage struct {
    Role    Role    `json:"role"`    // "user" or "assistant"
    Content Content `json:"content"`
}

type PromptHandler = func(ctx context.Context, req *GetPromptRequest) (*GetPromptResult, error)
```

### Transport Types

All transports implement:

```go
type Transport interface {
    Connect(ctx context.Context) (Connection, error)
}
```

| Transport | Direction | Purpose |
|-----------|-----------|---------|
| `StdioTransport` | Server | Runs over stdin/stdout |
| `CommandTransport` | Client | Spawns a server process, communicates via its stdin/stdout |
| `IOTransport` | Both | Custom `io.Reader` / `io.Writer` pair |
| `InMemoryTransport` | Both | In-process testing; use `NewInMemoryTransports()` to create a connected pair |
| `LoggingTransport` | Both | Wraps another transport with `slog.Logger` for debugging |
| `SSEClientTransport` | Client | Connects to an SSE-based server (`BaseURL` field) |
| `SSEServerTransport` | Server | SSE endpoint; implements `http.Handler` via `ServeHTTP` |
| `StreamableClientTransport` | Client | Connects to a Streamable HTTP server (`BaseURL` field) |
| `StreamableServerTransport` | Server | Streamable HTTP endpoint; implements `http.Handler` via `ServeHTTP` |

HTTP handler constructors:

```go
func NewSSEHandler(getServer func(*http.Request) *Server, opts *SSEOptions) *SSEHandler
func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler
```

### Middleware

```go
type Middleware = func(method string, next MethodHandler) MethodHandler
type MethodHandler = func(ctx context.Context, method string, req Request) (Result, error)

// Add to client or server:
func (s *Server) AddReceivingMiddleware(middleware ...Middleware)
func (s *Server) AddSendingMiddleware(middleware ...Middleware)
func (c *Client) AddReceivingMiddleware(middleware ...Middleware)
func (c *Client) AddSendingMiddleware(middleware ...Middleware)
```

### Implementation

```go
type Implementation struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}
```

### Event Store (Streamable HTTP resumability)

```go
type EventStore interface {
    Open(ctx context.Context, sessionID, streamID string) error
    Append(ctx context.Context, sessionID, streamID string, data []byte) error
    After(ctx context.Context, sessionID, streamID string, index int) iter.Seq2[[]byte, error]
    SessionClosed(ctx context.Context, sessionID string) error
}

type MemoryEventStore struct{}
func NewMemoryEventStore(opts *MemoryEventStoreOptions) *MemoryEventStore
```

### Auth (`auth` package)

**Server-side bearer token verification:**

```go
type TokenVerifier func(ctx context.Context, token string, req *http.Request) (*TokenInfo, error)

type TokenInfo struct {
    Scopes     []string
    Expiration time.Time
    UserID     string
    Extra      map[string]any
}

func RequireBearerToken(verifier TokenVerifier, opts *RequireBearerTokenOptions) func(http.Handler) http.Handler
func ProtectedResourceMetadataHandler(metadata *oauthex.ProtectedResourceMetadata) http.Handler
func TokenInfoFromContext(ctx context.Context) *TokenInfo
```

**Client-side authorization code flow:**

```go
type OAuthHandler interface {
    TokenSource(ctx context.Context) (oauth2.TokenSource, error)
    Authorize(ctx context.Context, req *http.Request, resp *http.Response) error
}

type AuthorizationCodeHandler struct { /* unexported */ }
func NewAuthorizationCodeHandler(config *AuthorizationCodeHandlerConfig) (*AuthorizationCodeHandler, error)

type AuthorizationCodeFetcher func(ctx context.Context, args *AuthorizationArgs) (*AuthorizationResult, error)

// Discovery
func GetAuthServerMetadata(ctx context.Context, issuerURL string, httpClient *http.Client) (*oauthex.AuthServerMeta, error)
```

### JSON-RPC (`jsonrpc` package)

Low-level JSON-RPC v2 primitives for custom transport authors:

```go
type Message = jsonrpc2.Message
type Request = jsonrpc2.Request
type Response = jsonrpc2.Response
type ID = jsonrpc2.ID
type Error = jsonrpc2.WireError

func EncodeMessage(msg Message) ([]byte, error)
func DecodeMessage(data []byte) (Message, error)
func MakeID(v any) (ID, error)

// Standard error codes
const (
    CodeParseError     = -32700
    CodeInvalidRequest = -32600
    CodeMethodNotFound = -32601
    CodeInvalidParams  = -32602
    CodeInternalError  = -32603
)
```

### Logging Integration

The SDK provides an `slog.Handler` implementation that forwards Go structured logs to the MCP client as logging messages:

```go
type LoggingHandler struct{}
func NewLoggingHandler(ss *ServerSession, opts *LoggingHandlerOptions) *LoggingHandler
// Implements slog.Handler: Enabled, Handle, WithAttrs, WithGroup
```

---

## Common Patterns

### Basic Server with Typed Tool

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

### Basic Client

```go
package main

import (
    "context"
    "log"
    "os/exec"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
    ctx := context.Background()

    client := mcp.NewClient(
        &mcp.Implementation{Name: "mcp-client", Version: "v1.0.0"}, nil,
    )
    transport := &mcp.CommandTransport{Command: exec.Command("myserver")}
    session, err := client.Connect(ctx, transport, nil)
    if err != nil {
        log.Fatal(err)
    }
    defer session.Close()

    res, err := session.CallTool(ctx, &mcp.CallToolParams{
        Name:      "greet",
        Arguments: map[string]any{"name": "world"},
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.IsError {
        log.Fatal("tool failed")
    }
    for _, c := range res.Content {
        log.Print(c.(*mcp.TextContent).Text)
    }
}
```

### HTTP Server (SSE)

```go
server := mcp.NewServer(
    &mcp.Implementation{Name: "my-server", Version: "v1.0.0"}, nil,
)
// ... register tools, resources, prompts ...

handler := mcp.NewSSEHandler(func(req *http.Request) *mcp.Server {
    return server
}, nil)
http.Handle("/sse", handler)
log.Fatal(http.ListenAndServe(":8080", nil))
```

### HTTP Server (Streamable HTTP — preferred for new deployments)

```go
handler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
    return server
}, nil)
http.Handle("/mcp", handler)
```

### Server-Side OAuth Protection

```go
import "github.com/modelcontextprotocol/go-sdk/auth"

verifier := func(ctx context.Context, token string, req *http.Request) (*auth.TokenInfo, error) {
    // Validate JWT, extract claims
    return &auth.TokenInfo{Scopes: scopes, UserID: uid}, nil
}

mcpHandler := mcp.NewStreamableHTTPHandler(func(req *http.Request) *mcp.Server {
    return server
}, nil)

protected := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
    ResourceMetadataURL: "https://example.com/.well-known/oauth-protected-resource",
    Scopes:              []string{"mcp:read"},
})(mcpHandler)

mux.Handle("/mcp", protected)
mux.Handle("/.well-known/oauth-protected-resource",
    auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{ /* ... */ }))
```

### In-Memory Testing

```go
t1, t2 := mcp.NewInMemoryTransports()

// Connect server first, then client
serverSess, _ := server.Connect(ctx, t1, nil)
clientSess, _ := client.Connect(ctx, t2, nil)
defer serverSess.Close()
defer clientSess.Close()

// Use clientSess to call tools registered on server
```

---

## Integration Notes

### Tool Handler Signatures

Two handler patterns are supported:

1. **Low-level** — receives raw JSON arguments:
   ```go
   func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error)
   ```

2. **Generic (typed)** — `AddTool[In, Out]` automatically generates JSON schemas from Go struct types, validates inputs, and deserializes arguments:
   ```go
   func(ctx context.Context, req *mcp.CallToolRequest, input In) (*mcp.CallToolResult, Out, error)
   ```

Use the generic form wherever possible — it eliminates manual schema authoring and validation.

### Schema Tags

The `jsonschema` struct tag provides field descriptions for the generated JSON schema:

```go
type Input struct {
    Name string `json:"name" jsonschema:"the name of the person"`
    Age  int    `json:"age,omitempty" jsonschema:"the person's age in years"`
}
```

### Transport Selection Guide

| Scenario | Transport |
|----------|-----------|
| CLI tools, subprocess-based MCP | `StdioTransport` (server) + `CommandTransport` (client) |
| Web services, remote MCP | `StreamableHTTPHandler` + `StreamableClientTransport` |
| Legacy SSE clients | `SSEHandler` + `SSEClientTransport` |
| Unit/integration tests | `NewInMemoryTransports()` |
| Custom I/O (pipes, WebSockets) | `IOTransport` or build via `jsonrpc` package |

### Logging Levels

Custom logging levels are defined bridging `slog` to MCP:

```go
LevelDebug, LevelInfo, LevelNotice, LevelWarning,
LevelError, LevelCritical, LevelAlert, LevelEmergency
```

Use `NewLoggingHandler(serverSession, nil)` to create an `slog.Handler` that forwards application logs through the MCP session.

### Client Registration Strategies (OAuth)

The auth package supports three client registration strategies, tried in order:

1. **Client ID Metadata Document** — per `draft-ietf-oauth-client-id-metadata-document` (see [client.dev](https://client.dev/))
2. **Preregistration** — static `ClientID` + `ClientSecret`
3. **Dynamic Client Registration** — per RFC 7591

### Third-Party Alternatives

The official SDK acknowledges these third-party Go MCP SDKs:
- [mcp-go](https://github.com/mark3labs/mcp-go) — by Ed Zynda
- [mcp-golang](https://github.com/metoro-io/mcp-golang)
- [go-mcp](https://github.com/ThinkInAIXYZ/go-mcp)

### Examples Directory

The repository includes example servers covering: `basic`, `completion`, `custom-transport`, `distributed`, `elicitation`, `everything`, `hello`, `memory`, `middleware`, `proxy`, `sequentialthinking`, `sse`, `toolschemas`, `auth/client`, `auth/server`, `auth/enterprise`, `client/listfeatures`, `client/loadtest`, `client/middleware`, `http`, `rate-limiting`.

---

**Source:** Extracted from [pkg.go.dev](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk), [GitHub README](https://github.com/modelcontextprotocol/go-sdk), and package documentation on June 14, 2026.

# CLI and MCP Integration

**Priority:** IMPORTANT

## Overview

ATCR exposes its functionality through two entry points: a CLI built with Cobra (`cmd/atcr`) and an MCP server exposed via stdio (`atcr serve`). Both paths share the same internal engine; the integration boundary is where user-facing concerns—flags, logging, exit codes, and stdio discipline—are handled before handing off to shared packages.

The structured-logging epic introduces `internal/log` and `internal/errors`, replacing ad-hoc `slog` construction. The logger is built once in the root command's `PersistentPreRunE` and propagated through `context.Context` to every handler. MCP mode adds a critical constraint: the stdio transport owns stdout, so all diagnostic output must route to stderr.

## Key Concepts

### Cobra Command Lifecycle

> Source: [cobra.md:Command struct (central building block)]

Cobra executes a command through ordered hooks. `PersistentPreRunE` is inherited by children, making it the single point to construct shared resources (logger, context, error classifier) before any handler runs.

### Context Propagation

> Source: [cobra.md:Execution and hierarchy]

`ExecuteContext(ctx)` runs the root command against `os.Args` with a context retrievable via `cmd.Context()` inside handlers. This is how the global timeout and the root logger flow into the fan-out engine.

### MCP Stdio Discipline

> Source: [mcp-go-sdk.md:Common Patterns]

The stdio transport owns stdout. Inside `serve` mode, all human-readable and log output must go to stderr. The engine receives a `*slog.Logger` injected from the entry point; handlers must not construct their own.

### Root Command Location

> Source: [codebase-discovery.json:ARCHITECTURE_NOTES]

There is no `cmd/atcr/root.go`; the root command is defined in `cmd/atcr/main.go:newRootCmd`. Flags and `PersistentPreRunE` must be added there.

### Thin Handler Pattern

> Source: [mcp-go-sdk.md:Common Patterns]

Each MCP tool handler parses typed args and calls the same internal package the CLI command calls. No logic in handlers. This constraint (Epic 1.0) means logging setup happens once, before the handler boundary.

## Code Examples

### Cobra Command Lifecycle

> Source: [cobra.md:Command struct (central building block)]

```go
type Command struct {
    // Run lifecycle hooks, in execution order:
    PersistentPreRunE  func(cmd *Command, args []string) error  // inherited by children
    PreRunE            func(cmd *Command, args []string) error  // local only
    RunE               func(cmd *Command, args []string) error  // main logic
    PostRunE           func(cmd *Command, args []string) error
    PersistentPostRunE func(cmd *Command, args []string) error
}
```

### MCP Server Over Stdio

> Source: [mcp-go-sdk.md:Creating and running a server over stdio]

```go
server := mcp.NewServer(&mcp.Implementation{Name: "atcr", Version: "1.0.0"}, nil)

if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
    log.Fatal(err)
}
```

## Quick Reference

| Concern | Location | Pattern |
|---------|----------|---------|
| Root command definition | `cmd/atcr/main.go:newRootCmd` | Declare `LOG_LEVEL` env, `--log-format` persistent flag, `PersistentPreRunE` |
| Logger construction | `cmd/atcr/main.go:PersistentPreRunE` | `internal/log.New`, read `LOG_LEVEL`, store in `cmd.Context()` |
| Logger retrieval in commands | `cmd/atcr/*.go` | Retrieve from `cmd.Context()` |
| Logger injection in MCP | `cmd/atcr/serve.go:newServeCmd` | Pass root logger to `mcp.Serve` |
| Engine receives logger | `internal/mcp/server.go:buildServer` | Accepts `*slog.Logger`, injects into engine struct |
| Stdio transport ownership | `atcr serve` mode | stdout = transport; stderr = all log/human output |
| Context for global timeout | `ExecuteContext(ctx)` | Root context carries timeout into fan-out engine |

## Related Documentation

- [Core Logging Package](core-logging-package.md) — `internal/log` construction, `LOG_LEVEL` semantics
- [Error Classification System](error-classification-system.md) — `internal/errors` taxonomy and exit-code mapping
- [Request Correlation](request-correlation.md) — request ID propagation through `context.Context`

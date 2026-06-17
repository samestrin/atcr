# Core Logging Package (internal/log)

**Priority:** CRITICAL

## Overview

The `internal/log` package consolidates ATCR's diagnostic output by providing a shared structured logging layer built on `log/slog`. It establishes a consistent logger creation API, context-based propagation, and request correlation IDs across all packages.

The codebase already uses structured logging via `log/slog`, but inconsistently. The MCP subsystem has the most mature injection pattern (nil-safe fallback to a discard logger) and serves as the model. Other packages such as `internal/payload` fall back to `slog.Default()`, which relies on global state and cannot be captured in tests — that pattern is explicitly rejected.

A companion `internal/errors` package handles error classification. Together they eliminate security risks from path and secret leakage in log output.

## Key Concepts

### Standard Library Foundation

ATCR's dependency constraint (Epic 1.0: keep the dependency tree small) means `internal/log` is built entirely on stdlib. Key packages: `log/slog` (structured logging), `context` (logger propagation), `io` (writer abstraction), `os` (stderr as default sink), and `strings`/`regexp` (redaction patterns).

> Source: [standard-library.md:Go Standard Library Usage]

### Core API

```go
// internal/log/log.go
package log

func New(level string, format string, w io.Writer) (*slog.Logger, error)
func LevelFromString(s string) (slog.Level, error)

// Context helpers
func FromContext(ctx context.Context) *slog.Logger
func NewContext(ctx context.Context, logger *slog.Logger) context.Context
```

Levels: `debug`, `info`, `warn`, `error` (default: `info`). Formats: `text` (default) or `json`. Sink: writes to the supplied `io.Writer`.

> Source: [plan.md:Technical Planning Notes]

### Nil-Safe Logger Injection (Pattern to Replicate)

The MCP server injects a `*slog.Logger` into the engine struct at construction time. When nil, it falls back to a discard logger, preventing panics and allowing tests to omit the logger. This is the pattern `internal/log` consumers should adopt.

> Source: [codebase-discovery.json:Existing Patterns — Logger Injection with Nil-Safe Fallback]

### slog.Default() Fallback (Pattern to Reject)

The payload package's `gitRunner` falls back to `slog.Default()` when no logger is injected. This uses global state and cannot be captured in tests. The new `internal/log` package requires explicit logger injection; this pattern must not be followed.

> Source: [codebase-discovery.json:Existing Patterns — slog.Default() Fallback]

### Global Default Policy

`cmd/atcr` creates one logger and passes it down through context. Packages never call `slog.Default()` in production.

> Source: [plan.md:Technical Planning Notes]

## Code Examples

### Nil-Safe Logger Accessor

Verbatim from `internal/mcp/handlers.go`:

```go
func (e *engine) logger() *slog.Logger {
    if e.log == nil {
        return slog.New(slog.NewTextHandler(io.Discard, nil))
    }
    return e.log
}
```

> Source: [codebase-discovery.json:Reusable Components — Nil-Safe Logger Pattern]

### Logger Construction

Verbatim from `original-requirements.md`:

```go
// internal/log/log.go
package log

func New(level string, format string, w io.Writer) (*slog.Logger, error)
func LevelFromString(s string) (slog.Level, error)

// Context helpers
func FromContext(ctx context.Context) *slog.Logger
func NewContext(ctx context.Context, logger *slog.Logger) context.Context
```

> Source: [original-requirements.md:Core Design — Core API]

## Quick Reference

| Concept | Value | Source |
|---------|-------|--------|
| Framework | `log/slog` (stdlib) | standard-library.md |
| Go version | 1.25+ | standard-library.md |
| Default level | `info` | plan.md |
| Supported levels | debug, info, warn, error | plan.md |
| Supported formats | text, json | plan.md |
| Default sink | `io.Writer` (stderr in cmd/atcr) | plan.md |
| Injection pattern | nil-safe fallback to discard logger | codebase-discovery.json |
| Context helpers | `FromContext`, `NewContext` | plan.md |
| Anti-pattern | `slog.Default()` fallback (payload pkg) | codebase-discovery.json |
| New capabilities | `WithReviewID`, `WithAgent` | codebase-discovery.json |

## Related Documentation

- [Epic 4.0 Plan](../plan.md) — full structured logging and error classification epic
- [Codebase Discovery](../codebase-discovery.json) — existing logging patterns and reusable components
- [Standard Library Reference](../../../specifications/packages/standard-library.md) — Go stdlib packages used by internal/log
- [Error Classification System](./error-classification-system.md) — companion `internal/errors` package

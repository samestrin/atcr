# Task 01: Create Core Logging API (`internal/log/log.go`)

**Source:** Plan 4.0 – Debt Item #1
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
ATCR currently has no consistent logging strategy. Diagnostic output is emitted ad hoc across the CLI and engine, creating five concrete problems: (1) no level control despite documented `LOG_LEVEL`, (2) inconsistent sinks (MCP uses `slog`, CLI uses `os.Stderr`), (3) no error classification taxonomy, (4) no request correlation across agents, (5) security risks from path/secret leakage. Every component chooses its own output mechanism, making it impossible to capture, filter, or test log output uniformly. A shared `internal/log` package must become the single way ATCR emits diagnostics.

## Solution Overview
Create the `internal/log` package with a minimal core API built entirely on `log/slog` (stdlib). The package exposes:
- `New(level, format string, w io.Writer) (*slog.Logger, error)` — constructs a configured logger with level filtering and format selection (text/json).
- `LevelFromString(s string) (slog.Level, error)` — parses level strings (`debug`, `info`, `warn`, `error`); defaults to `info`.
- `FromContext(ctx context.Context) *slog.Logger` / `NewContext(ctx context.Context, logger *slog.Logger) context.Context` — context propagation helpers.

This is the foundation for the entire Epic 4.0. Every subsequent task (redaction, correlation, error taxonomy, CLI/engine wiring) depends on this package existing.

## Technical Implementation
### Steps
1. Create `internal/log/log.go` with the `log` package declaration. Define the core API: `New`, `LevelFromString`, `FromContext`, `NewContext`. Implement `New` to select between `slog.NewTextHandler` and `slog.NewJSONHandler` based on the `format` parameter, wired to the supplied `io.Writer`. Implement `LevelFromString` to map `debug`→`slog.LevelDebug`, `info`→`slog.LevelInfo`, `warn`→`slog.LevelWarn`, `error`→`slog.LevelError`; return an error for unrecognized strings. Default level to `info` when an empty string is passed.
2. Define a private `contextKey` type (unexported struct) for the context key to avoid collisions. Implement `NewContext` to store the `*slog.Logger` in the context using this key. Implement `FromContext` to retrieve the logger; return a discard logger (`slog.New(slog.NewTextHandler(io.Discard, nil))`) when no logger is present in the context — this follows the nil-safe fallback pattern established in `internal/mcp/handlers.go`.
3. Create `internal/log/log_test.go` with table-driven tests covering: valid levels (`debug`, `info`, `warn`, `error`), empty string (defaults to `info`), invalid level strings (returns error), text format output, JSON format output, `FromContext` with no logger returns discard logger, `NewContext`/`FromContext` round-trip preserves the logger, custom `io.Writer` sink receives output. Target 100% coverage of level parsing and sink wiring (AC8).

## Files to Create/Modify
- `internal/log/log.go` – create
- `internal/log/log_test.go` – create

## Documentation Links
- [Core Logging Package](../documentation/core-logging-package.md)
- [Testing Patterns](../documentation/testing-patterns.md)

## Related Files (from codebase-discovery.json)
- `internal/mcp/handlers.go` — existing nil-safe logger injection pattern to replicate
- `internal/mcp/server.go` — existing logger injection site
- `internal/payload/diff.go` — existing `slog.Default()` fallback (anti-pattern; must not be followed)
- `internal/payload/builder.go` — constructs `gitRunner` without logger; future consumer
- `internal/fanout/engine.go` — future consumer of `WithAgent` (Task 03)
- `internal/llmclient/client.go` — existing HTTP error classification; future consumer
- `cmd/atcr/main.go` — CLI entry point; future `LOG_LEVEL`/`--log-format` wiring (Task 07)
- `cmd/atcr/review.go` — future `WithReviewID` wiring (Task 14)

## Success Criteria
- [ ] `internal/log/log.go` compiles with no external dependencies (stdlib only)
- [ ] `New("info", "text", os.Stderr)` returns a non-nil `*slog.Logger`
- [ ] `New("debug", "json", &buf)` writes JSON-formatted output to the supplied writer
- [ ] `LevelFromString("debug")` returns `slog.LevelDebug` with no error
- [ ] `LevelFromString("")` returns `slog.LevelInfo` with no error
- [ ] `LevelFromString("bogus")` returns a non-nil error
- [ ] `FromContext(context.Background())` returns a non-nil discard logger (no panic)
- [ ] `FromContext(NewContext(ctx, logger))` returns the same logger instance
- [ ] `go test ./internal/log/...` passes with 100% coverage of level parsing and sink wiring
- [ ] No `slog.Default()` usage anywhere in `internal/log/`

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestLevelFromString_ValidLevels` — table-driven: `debug`, `info`, `warn`, `error` each return expected `slog.Level`
- `TestLevelFromString_EmptyDefaultsToInfo` — empty string returns `slog.LevelInfo`, nil error
- `TestLevelFromString_InvalidReturnsError` — `"bogus"`, `"TRACE"`, `"123"` each return non-nil error
- `TestNew_TextFormat` — construct with `"text"`, log a message, assert writer contains expected text output
- `TestNew_JSONFormat` — construct with `"json"`, log a message, assert writer receives valid JSON with `level`, `msg` keys
- `TestNew_LevelFiltering` — construct with `"warn"`, log at `info`, assert writer is empty; log at `error`, assert writer is non-empty
- `TestNew_InvalidLevelReturnsError` — `New("bogus", "text", os.Stderr)` returns non-nil error
- `TestNew_InvalidFormatReturnsError` — `New("info", "xml", os.Stderr)` returns non-nil error
- `TestFromContext_EmptyContext` — `FromContext(context.Background())` returns non-nil logger, does not panic
- `TestNewContext_FromContext_RoundTrip` — store a logger, retrieve it, assert pointer equality
- `TestFromContext_DiscardLoggerNoOutput` — logger from empty context writes nothing (discard sink)

**Integration Tests:**
- None for this task. Integration tests for context threading through CLI/engine belong to Tasks 07-09.

**Test Files:**
- `internal/log/log_test.go`

## Risk Mitigation
| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| `contextKey` collision with other packages | Low | Low | Use unexported struct type as key, not a string |
| Future packages forget to use `FromContext` and fall back to `slog.Default()` | Medium | Medium | Establish pattern early; add linter guidance in Task 10 (fanout/logger wiring) |
| Discard logger in `FromContext` masks missing logger bugs | Low | Low | Return discard (not nil) to prevent panics; matches existing `internal/mcp` pattern |

## Dependencies
- None. This is the foundation task; all other tasks in Epic 4.0 depend on this one.

## Definition of Done
- [ ] `internal/log/log.go` implements `New`, `LevelFromString`, `FromContext`, `NewContext`
- [ ] `internal/log/log_test.go` covers all public functions with table-driven tests
- [ ] `go test ./internal/log/...` passes with 100% coverage of level parsing and sink wiring
- [ ] `go vet ./internal/log/...` passes with no warnings
- [ ] No external dependencies added (stdlib only: `log/slog`, `context`, `io`, `strings`)
- [ ] Code follows nil-safe fallback pattern established in `internal/mcp/handlers.go`
- [ ] No `slog.Default()` usage in `internal/log/` package

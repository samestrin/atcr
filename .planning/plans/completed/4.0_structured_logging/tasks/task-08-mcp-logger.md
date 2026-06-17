# Task 08: MCP Server Logger Reuse

**Source:** Plan 4.0 – Debt Item #8
**Priority:** P1 | **Effort:** S | **Type:** Refactor

## Problem Statement
`cmd/atcr/serve.go` currently constructs its own local `slog.Logger` instead of using the root logger built by `PersistentPreRunE`. This means MCP mode bypasses the shared logger configuration (level, format, redaction) and any correlation context set in the CLI layer.

## Solution Overview
Update `cmd/atcr/serve.go:newServeCmd` to retrieve the root logger from `cmd.Context()` via `log.FromContext` and pass it to `mcp.Serve`. Remove the local logger construction. The MCP server already accepts a `*slog.Logger` and injects it into the engine struct — it just needs to receive the right one.

This also validates that the MCP stdio discipline is preserved: stdout remains owned by the stdio transport, and all diagnostic output routes to stderr (the root logger's sink).

## Technical Implementation
### Steps
1. In `cmd/atcr/serve.go:newServeCmd`, replace the local `slog.New(...)` construction with `log.FromContext(cmd.Context())`.
2. Pass the retrieved logger to `mcp.Serve` (or the function that builds the MCP server).
3. Verify that `internal/mcp/server.go:buildServer` continues to accept the `*slog.Logger` and inject it into the engine struct — no changes needed there.
4. Confirm MCP stdio tests still pass (stdout protocol output is not polluted by log lines).

## Files to Create/Modify
- `cmd/atcr/serve.go` — modify (remove local logger, use context logger)

## Documentation Links
- [CLI and MCP Integration](../documentation/cli-mcp-integration.md)

## Related Files (from codebase-discovery.json)
- `cmd/atcr/serve.go:newServeCmd` — currently builds a local logger; must use context logger
- `internal/mcp/server.go:buildServer` — accepts `*slog.Logger`, injects into engine (no change needed)
- `internal/mcp/handlers.go` — existing nil-safe logger pattern (preserved)

## Success Criteria
- [ ] `cmd/atcr/serve.go` no longer constructs a local `slog.Logger`
- [ ] The root logger from context is passed to `mcp.Serve`
- [ ] MCP server injects the root logger into the engine struct
- [ ] MCP stdio tests pass (stdout is transport-only)
- [ ] `LOG_LEVEL` and `--log-format` flags affect MCP mode output
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go test ./internal/mcp/...` passes

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestServeCmd_UsesContextLogger` — construct serve command with a context logger at debug level, invoke it, verify the logger passed to the MCP server is the context logger (not a default one)
- Existing MCP tests should continue to pass without modification

**Test Files:**
- `cmd/atcr/serve_test.go` (create or modify)

## Risk Mitigation
- **Nil-safe fallback**: If `FromContext` returns a discard logger (context has no logger), the MCP server's existing nil-safe fallback in `handlers.go` still works. The `PersistentPreRunE` (Task 07) ensures the logger is always in context before `serve` runs.
- **Stdio discipline**: The root logger writes to `os.Stderr`, which is correct for MCP mode. No stdout pollution.
- **No structural changes to MCP**: The MCP server already accepts a `*slog.Logger`; this task only changes where that logger comes from.

## Dependencies
- Task 07 (cli-logger-construction) — root logger is in context
- Task 04 (logging-package validation) — `internal/log` is stable

## Definition of Done
- [ ] `cmd/atcr/serve.go` uses context logger
- [ ] Local logger construction removed
- [ ] `go build ./cmd/atcr/...` succeeds
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go test ./internal/mcp/...` passes
- [ ] MCP stdio tests verify stdout discipline

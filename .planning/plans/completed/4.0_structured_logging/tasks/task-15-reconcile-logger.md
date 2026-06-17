# Task 15: Reconcile Command Logger Wiring

**Source:** Plan 4.0 – Phase 3 CLI Wiring / codebase-discovery.json integration point
**Priority:** P2 | **Effort:** S | **Type:** Refactor

## Problem Statement

`cmd/atcr/reconcile.go` currently writes diagnostics directly via `cmd.ErrOrStderr()` for the `--require-verified` warning and routes scorecard diagnostics through the command's stderr writer. These writes bypass the shared logger, so they do not honor `LOG_LEVEL`, `--log-format`, redaction, or correlation IDs.

## Solution Overview

Update `cmd/atcr/reconcile.go:runReconcile` to retrieve the root logger from the command context via `log.FromContext(cmd.Context())` and route diagnostics through it. Replace direct `cmd.ErrOrStderr()` writes for warnings with `logger.Warn(...)`. Keep user-facing result output (e.g., the reconcile summary printed to stdout) unchanged; only diagnostics and warnings move to the logger.

## Technical Implementation

### Steps

1. In `cmd/atcr/reconcile.go:runReconcile`, retrieve the context logger at the start:
   ```go
   logger := log.FromContext(cmd.Context())
   ```
2. Replace the `--require-verified` warning written to `cmd.ErrOrStderr()` with a structured log line:
   ```go
   logger.Warn("require_verified: verify stage not complete", "detail", verr.Error())
   ```
3. Route scorecard diagnostics through the logger's `Warn`/`Debug` levels as appropriate:
   - High-level scorecard summary: `logger.Info` or `logger.Warn`
   - Path-bearing details: `logger.Debug` (to satisfy the TD item: keep path-bearing detail at debug level only)
4. Ensure `cmd/atcr/reconcile.go` does not construct a new `slog.Logger` and does not call `slog.Default()`.

## Files to Create/Modify

- `cmd/atcr/reconcile.go` — modify (use context logger for diagnostics)
- `cmd/atcr/reconcile_test.go` — modify/add tests to assert diagnostics flow through the captured logger, not `os.Stderr`

## Documentation Links

- [CLI and MCP Integration](../documentation/cli-mcp-integration.md)
- [Core Logging Package](../documentation/core-logging-package.md)
- [Request Correlation](../documentation/request-correlation.md)

## Related Files (from codebase-discovery.json)

- `cmd/atcr/reconcile.go:runReconcile` — integration point for context logger
- `internal/scorecard` — scorecard diagnostics sink; may need to accept a logger or continue using the injected `Diag` writer
- `internal/reconcile` — reconcile orchestration used by both CLI and MCP

## Success Criteria

- [ ] `cmd/atcr/reconcile.go:runReconcile` retrieves the logger from command context
- [ ] `--require-verified` warning is emitted via `logger.Warn` (not `cmd.ErrOrStderr()`)
- [ ] Scorecard diagnostics route through the context logger
- [ ] Path-bearing scorecard details are logged at `Debug` level
- [ ] User-facing reconcile summary output remains on stdout
- [ ] No local `slog.Logger` construction in `cmd/atcr/reconcile.go`
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go test ./...` passes (no regressions)

## Manual Code Review

- [ ] Codebase has been reviewed

## Test Strategy

**Unit Tests:**

- `TestRunReconcile_UsesContextLogger` — run reconcile with a context logger captured in a `bytes.Buffer`, assert warning and diagnostics appear in the buffer
- `TestRunReconcile_RequireVerifiedWarning` — set `--require-verified` without a gate or verify results, assert `logger.Warn` output contains the warning (not `os.Stderr`)
- `TestRunReconcile_NoSlogDefault` — static check: `slog.Default` and `slog.New` do not appear in `cmd/atcr/reconcile.go`

**Test Files:**

- `cmd/atcr/reconcile_test.go`

## Risk Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Scorecard output format changes for CLI users | Low | Medium | Keep user-facing stdout summary unchanged; only diagnostics move to the logger |
| `--require-verified` warning no longer visible by default | Low | Medium | `Warn` level is enabled at default `info` level, so the warning remains visible |
| MCP reconcile path diverges from CLI path | Low | High | Use the same internal `reconcile.RunReconcile` call; only the CLI's warning routing changes. MCP handlers already use the engine logger. |

## Dependencies

- Task 01 (core-logging-api) — `log.FromContext`
- Task 07 (cli-logger-construction) — root logger is in context
- Task 14 (review-correlation) — same CLI wiring pattern (optional; can run in parallel)

## Definition of Done

- [ ] `cmd/atcr/reconcile.go` retrieves context logger
- [ ] Diagnostics and warnings route through the logger
- [ ] Tests verify logger-based output
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go vet ./cmd/atcr/...` clean
- [ ] `go test ./...` passes

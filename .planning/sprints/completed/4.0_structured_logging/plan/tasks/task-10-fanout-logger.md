# Task 10: Fanout Engine Logger Wiring

**Source:** Plan 4.0 – Debt Item #10
**Priority:** P1 | **Effort:** M | **Type:** Add

## Problem Statement
The fanout engine (`internal/fanout`) has no logger field. `ExecuteReview` and `invokeSkeptic` construct the engine without a logger. Agent invocations produce no structured log output, and skeptic failures write directly to `os.Stderr` via `fmt.Fprintf`, bypassing the shared logger and leaking paths at default log levels.

## Solution Overview
1. Add a `*slog.Logger` field and a `WithLogger` `EngineOption` to `internal/fanout/engine.go`.
2. In `invokeAgent`, call `log.WithAgent(logger, a.Name)` before each agent invocation so all log lines within that agent carry the agent name.
3. In `ExecuteReview` (`internal/fanout/review.go`), retrieve the logger from context and pass it to `NewEngine` via `WithLogger`.
4. In `internal/verify/invoke.go:invokeSkeptic`, pass the context logger into the throwaway `fanout.Engine` and migrate `logSkepticFailure` from `fmt.Fprintf(os.Stderr, ...)` to the injected logger.
5. Migrate the highest-risk direct stderr writes in `internal/fanout/review.go` (snapshot/jail warnings) to the injected logger, with path details at debug level.

## Technical Implementation
### Steps
1. In `internal/fanout/engine.go`:
   - Add `log *slog.Logger` field to the `Engine` struct.
   - Add `func WithLogger(l *slog.Logger) EngineOption` that sets `e.log = l`.
   - Add a `logger()` method with nil-safe fallback (matching `internal/mcp/handlers.go` pattern).
   - In `invokeAgent`, at the start of each invocation:
     ```go
     agentLogger := log.WithAgent(e.logger(), a.Name)
     ```
     Use `agentLogger` for all log calls within the agent invocation.
2. In `internal/fanout/review.go:ExecuteReview`:
   - Retrieve logger from context: `logger := log.FromContext(ctx)`.
   - Pass to engine: `NewEngine(..., WithLogger(logger))`.
   - Replace direct `fmt.Fprintf(os.Stderr, ...)` calls with `logger.Warn(...)` or `logger.Debug(...)`.
3. In `internal/verify/invoke.go:invokeSkeptic`:
   - Retrieve logger from context.
   - Pass to throwaway engine: `NewEngine(..., WithLogger(logger))`.
   - Replace `logSkepticFailure`'s `fmt.Fprintf(os.Stderr, ...)` with `logger.Warn("skeptic failed", "error", err)`.
   - Put path-bearing details behind `logger.Debug(...)`.

## Files to Create/Modify
- `internal/fanout/engine.go` — modify (add logger field, `WithLogger`, `WithAgent` call)
- `internal/fanout/review.go` — modify (pass logger to engine, migrate stderr writes)
- `internal/verify/invoke.go` — modify (wire logger, migrate `logSkepticFailure`)
- `internal/fanout/engine_test.go` — modify (inject discard logger in existing tests)
- `internal/fanout/review_test.go` — modify (inject discard logger in existing tests)

## Documentation Links
- [Request Correlation](../documentation/request-correlation.md)
- [CLI and MCP Integration](../documentation/cli-mcp-integration.md)

## Related Files (from codebase-discovery.json)
- `internal/fanout/engine.go:Agent` (line 57) — carries `Name` for `WithAgent`
- `internal/fanout/engine.go:invokeAgent` — calls `WithAgent` before LLM call
- `internal/fanout/review.go:ExecuteReview` — passes logger to `NewEngine`
- `internal/verify/invoke.go:invokeSkeptic` (line 49) — needs logger wiring
- `internal/fanout/review.go` — direct `os.Stderr` writes to migrate
- `internal/verify/invoke.go:logSkepticFailure` — direct `os.Stderr` write to migrate

## Success Criteria
- [ ] `Engine` struct has a `*slog.Logger` field
- [ ] `WithLogger` option sets the logger
- [ ] `logger()` method returns discard logger when `log` is nil (no panic)
- [ ] `invokeAgent` calls `log.WithAgent` before each agent invocation
- [ ] `ExecuteReview` passes context logger to `NewEngine`
- [ ] `invokeSkeptic` passes context logger to throwaway engine
- [ ] `logSkepticFailure` uses injected logger instead of `os.Stderr`
- [ ] Direct `fmt.Fprintf(os.Stderr, ...)` in `internal/fanout/review.go` migrated to logger
- [ ] Path-bearing details in migrated messages are at debug level
- [ ] `go test ./internal/fanout/...` passes
- [ ] `go test ./internal/verify/...` passes
- [ ] All existing tests continue to pass

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestEngine_WithLogger` — construct engine with `WithLogger`, assert `logger()` returns the injected logger
- `TestEngine_NilLogger_ReturnsDiscard` — construct engine without `WithLogger`, assert `logger()` returns a non-nil discard logger (no panic)
- `TestInvokeAgent_AttachesAgentName` — invoke an agent, capture log output, assert `agent_name=<name>` appears in output
- `TestInvokeSkeptic_UsesLogger` — invoke skeptic with a context logger, capture output, assert skeptic failure appears in logger output (not stderr)
- Existing fanout and verify tests pass with discard logger injection

**Test Files:**
- `internal/fanout/engine_test.go` (modify)
- `internal/fanout/review_test.go` (modify)
- `internal/verify/invoke_test.go` (modify if exists)

## Risk Mitigation
- **Nil-safe fallback**: The `logger()` method returns a discard logger when nil, preventing panics. Matches the established `internal/mcp` pattern.
- **Test disruption**: All existing `Engine` constructions in tests need `WithLogger` or will get the discard fallback. Tests that assert on stderr output may need updating to capture logger output instead.
- **Stderr ownership in tests**: Tests that capture `os.Stderr` for assertion may need to switch to a `bytes.Buffer` sink wired to the logger. Audit test files for stderr capture patterns.
- **Incremental migration**: Only the highest-risk stderr writes are migrated. Lower-priority writes can be migrated incrementally.

## Dependencies
- Task 01 (core-logging-api) — `log.FromContext`
- Task 04 (logging-package validation) — `internal/log` stable
- Task 07 (cli-logger-construction) — context logger available
- Task 03 (request-correlation) — `WithReviewID` and `WithAgent` available

## Definition of Done
- [ ] `Engine` has logger field and `WithLogger` option
- [ ] `WithAgent` called in `invokeAgent`
- [ ] `ExecuteReview` passes context logger
- [ ] `invokeSkeptic` passes context logger
- [ ] Direct stderr writes migrated
- [ ] All fanout and verify tests pass
- [ ] `go test ./...` passes (no regressions)

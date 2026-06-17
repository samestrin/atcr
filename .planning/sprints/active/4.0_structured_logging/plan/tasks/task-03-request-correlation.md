# Task 03: Request Correlation — WithReviewID and WithAgent

**Source:** Plan 4.0 – Debt Item #3
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement

When debugging a specific review run, there is no way to correlate log lines across agents. A review spawns N agents, each with their own goroutine, but no shared identifier threads through the logs. Concurrent reviews interleave their output, making it impossible to determine which log lines belong to which review or which agent.

## Solution Overview

Create `internal/log/correlation.go` with two functions that derive a new `*slog.Logger` with an added structured attribute:

- `WithReviewID(logger *slog.Logger, reviewID string) *slog.Logger` — attaches `review_id` to every log line.
- `WithAgent(logger *slog.Logger, agentName string) *slog.Logger` — attaches `agent_name` to every log line.

Both return a new logger; neither mutates the input. Callers chain them as needed. The functions rely on `slog.Logger.With` to attach the attribute, which is the idiomatic stdlib approach.

This task depends on Task 01 (core-logging-api) for the `FromContext` / `NewContext` helpers and the base `internal/log` package structure.

## Technical Implementation

### Steps

1. **Create `internal/log/correlation.go`** with two exported functions:
   - `WithReviewID(logger *slog.Logger, reviewID string) *slog.Logger` — returns `logger.With("review_id", reviewID)`. If `logger` is nil, return nil (caller must handle via nil-safe pattern at call site, consistent with `internal/mcp/handlers.go`).
   - `WithAgent(logger *slog.Logger, agentName string) *slog.Logger` — returns `logger.With("agent_name", agentName)`. Same nil semantics.

2. **Add attribute key constants** to avoid string duplication across the package:
   - `const AttrReviewID = "review_id"`
   - `const AttrAgentName = "agent_name"`

3. **Write `internal/log/correlation_test.go`** with table-driven tests covering:
   - `WithReviewID` attaches `review_id` attribute and preserves existing attributes.
   - `WithAgent` attaches `agent_name` attribute and preserves existing attributes.
   - Chaining: `WithAgent(WithReviewID(logger, id), name)` produces both attributes.
   - Nil logger: both functions return nil when passed a nil logger.
   - Empty string: both functions still attach the attribute (empty string is a valid value).
   - Immutability: the original logger is not modified.

## Files to Create/Modify

- `internal/log/correlation.go` — create
- `internal/log/correlation_test.go` — create

## Documentation Links

- [Request Correlation](../documentation/request-correlation.md)
- [Core Logging Package](../documentation/core-logging-package.md)

## Related Files (from codebase-discovery.json)

- `cmd/atcr/review.go:72` — `runReview` calls `WithReviewID` after `PrepareReview` resolves `prep.ID` (line 151)
- `internal/fanout/engine.go:57` — `Agent` struct carries `Name` for `WithAgent`
- `internal/fanout/engine.go:380` — `invokeAgent` calls `WithAgent` before LLM call
- `internal/fanout/engine.go:205` — `NewEngine` must accept logger via `WithLogger` option (added in Task 10)
- `internal/fanout/engine.go:184` — `WithDispatcher` is the existing option pattern to follow for `WithLogger`
- `internal/verify/invoke.go:49` — `invokeSkeptic` needs logger wiring so skeptic failures appear in the correlated log stream
- `internal/verify/invoke.go:61` — constructs `fanout.NewEngine(cc, fanout.WithDispatcher(disp))` without a logger; must add `WithLogger` after Task 10

## Success Criteria

- [ ] `WithReviewID` returns a logger that emits `review_id=<value>` on every log line
- [ ] `WithAgent` returns a logger that emits `agent_name=<value>` on every log line
- [ ] Both functions return nil when passed a nil logger (no panic)
- [ ] Chaining `WithAgent(WithReviewID(logger, id), name)` produces both attributes
- [ ] Original logger is not mutated (immutability contract)
- [ ] `go test ./internal/log/...` passes

## Manual Code Review

- [ ] Codebase has been reviewed

## Test Strategy

**Unit Tests:**

- `TestWithReviewID_AttachesAttribute` — log a message, verify `review_id` appears in output
- `TestWithReviewID_PreservesExistingAttributes` — create logger with prior `.With(...)`, verify both old and new attributes present
- `TestWithAgent_AttachesAttribute` — log a message, verify `agent_name` appears in output
- `TestWithAgent_PreservesExistingAttributes` — same as above for agent
- `TestWithReviewID_NilLogger` — pass nil, verify return is nil, no panic
- `TestWithAgent_NilLogger` — pass nil, verify return is nil, no panic
- `TestChaining_ReviewIDAndAgent` — chain both, verify both attributes present in output
- `TestWithReviewID_EmptyString` — pass empty string, verify attribute still attached
- `TestWithAgent_EmptyString` — pass empty string, verify attribute still attached
- `TestOriginalLoggerUnmodified` — call `WithReviewID`, verify original logger does not have `review_id`

**Test Files:**

- `internal/log/correlation_test.go`

**Test approach:** Each test constructs a `slog.Logger` wired to a `bytes.Buffer` via `slog.NewTextHandler` (or `slog.NewJSONHandler` for attribute assertion). After logging, the buffer content is parsed to verify the expected attributes are present. This satisfies AC7 (no `slog.Default()` in tests).

## Risk Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Nil logger causes panic downstream | Low | High | Return nil on nil input; callers already use nil-safe pattern from `internal/mcp/handlers.go` |
| Attribute key typo causes inconsistent grep | Low | Medium | Use package-level constants (`AttrReviewID`, `AttrAgentName`) instead of inline strings |
| Task 01 not yet complete blocks this task | Medium | Low | This task depends on `internal/log` package existing; verify Task 01 done first |

## Dependencies

- Task 01 (core-logging-api) — `internal/log` package must exist with `FromContext` / `NewContext`

## Definition of Done

- [ ] `internal/log/correlation.go` implements `WithReviewID` and `WithAgent`
- [ ] Attribute key constants `AttrReviewID` and `AttrAgentName` exported
- [ ] `internal/log/correlation_test.go` passes with all cases green
- [ ] Nil logger handled without panic
- [ ] Chaining produces both attributes
- [ ] Original logger immutability verified
- [ ] `go vet ./internal/log/...` clean
- [ ] `go test ./internal/log/...` passes

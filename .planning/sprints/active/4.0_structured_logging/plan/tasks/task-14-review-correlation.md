# Task 14: Review Command Correlation (`WithReviewID`)

**Source:** Plan 4.0 – Phase 3 CLI Wiring
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement

Every log line emitted during a review must include the review ID so operators can `grep` all activity for a single run. The review ID is only available after `fanout.PrepareReview` returns `prep.ID`, so the CLI review command must attach it to the context logger at that point and store the correlated logger back in the command context.

## Solution Overview

Update `cmd/atcr/review.go:runReview` so that after `fanout.PrepareReview` succeeds, it calls `log.WithReviewID(logger, prep.ID)` and stores the returned logger in the command context with `log.NewContext`. Downstream calls (`fanout.ExecuteReview`, `reconcile.RunReconcile`, `verify.Verify`) then receive the correlated logger via `log.FromContext`.

## Technical Implementation

### Steps

1. In `cmd/atcr/review.go:runReview`, retrieve the root logger from the command context after flag parsing:
   ```go
   logger := log.FromContext(cmd.Context())
   ```
2. After `fanout.PrepareReview(ctx, cfg, req)` returns `prep`:
   ```go
   logger = log.WithReviewID(logger, prep.ID)
   ctx = log.NewContext(ctx, logger)
   ```
3. Use the updated `ctx` for all subsequent calls in `runReview`:
   - `fanout.ExecuteReview(ctx, llmclient.New(), prep)`
   - `reconcile.RunReconcile(ctx, result.Dir, nil, ...)`
   - `verify.Verify(ctx, ".", result.Dir, ...)`
4. Verify that `cmd/atcr/review.go` no longer constructs loggers locally or writes diagnostics directly to `os.Stderr`; all diagnostics flow through the context logger.

## Files to Create/Modify

- `cmd/atcr/review.go` — modify (attach `review_id` after `PrepareReview`)
- `cmd/atcr/review_test.go` — modify/add tests for context logger propagation and `review_id` attachment

## Documentation Links

- [Request Correlation](../documentation/request-correlation.md)
- [CLI and MCP Integration](../documentation/cli-mcp-integration.md)
- [Core Logging Package](../documentation/core-logging-package.md)

## Related Files (from codebase-discovery.json)

- `cmd/atcr/review.go:runReview` (line 72) — integration point for `WithReviewID`
- `cmd/atcr/review.go:151` — `fanout.PrepareReview` returns `prep.ID`
- `internal/fanout/review.go:ExecuteReview` — receives context and passes logger to engine (Task 10)
- `internal/payload/builder.go:BuildEntries` — payload logs should inherit the same context (Task 09)

## Success Criteria

- [ ] `cmd/atcr/review.go:runReview` retrieves the root logger from context
- [ ] After `PrepareReview`, the logger carries `review_id=<prep.ID>`
- [ ] The correlated logger is stored back in the context used for downstream calls
- [ ] `fanout.ExecuteReview`, `reconcile.RunReconcile`, and `verify.Verify` receive the correlated context
- [ ] No local logger construction remains in `cmd/atcr/review.go`
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go test ./...` passes (no regressions)

## Manual Code Review

- [ ] Codebase has been reviewed

## Test Strategy

**Unit Tests:**

- `TestRunReview_AttachesReviewID` — mock `PrepareReview` to return a known ID, run `runReview`, capture the logger passed to downstream calls (via a fake completer or context inspection), and assert `review_id=<known>` is present.
- `TestRunReview_ContextLoggerFlowsToExecuteReview` — verify that the same logger instance/context flows from `WithReviewID` into `ExecuteReview`.
- `TestRunReview_NoLocalLogger` — static check: `slog.New` and `slog.Default` do not appear in `cmd/atcr/review.go`.

**Test Files:**

- `cmd/atcr/review_test.go`

## Risk Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| `PrepareReview` is called before review ID exists | N/A | N/A | This is the correct timing; payload-building logs will inherit the root logger without `review_id` unless `PrepareReview` internally uses the context logger passed into it |
| Payload logs lack `review_id` because they run inside `PrepareReview` | Medium | Low | Documented gap; `BuildEntries` should read `log.FromContext(ctx)` so any logs emitted during payload building share the root context. Full correlation inside `PrepareReview` may require a follow-up task if the root context does not already carry the review ID. |
| Downstream call uses stale `cmd.Context()` instead of updated `ctx` | Medium | Medium | Reassign `ctx` after `log.NewContext` and explicitly pass it to all downstream functions; do not reuse `cmd.Context()` after the assignment |

## Dependencies

- Task 01 (core-logging-api) — `log.FromContext`, `log.NewContext`, `log.WithReviewID`
- Task 03 (request-correlation) — `log.WithReviewID` implementation
- Task 07 (cli-logger-construction) — root logger is in context before `runReview` runs
- Task 09 (payload-stderr) — payload builder reads context logger so payload logs share the same stream

## Definition of Done

- [ ] `cmd/atcr/review.go` attaches `review_id` after `PrepareReview`
- [ ] Correlated logger is stored in context for downstream calls
- [ ] All downstream calls use the correlated context
- [ ] Tests verify `review_id` attachment
- [ ] `go test ./cmd/atcr/...` passes
- [ ] `go vet ./cmd/atcr/...` clean
- [ ] `go test ./...` passes

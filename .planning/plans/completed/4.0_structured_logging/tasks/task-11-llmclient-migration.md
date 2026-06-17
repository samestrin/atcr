# Task 11: llmclient Error Classification Migration

**Source:** Plan 4.0 – Debt Item #11
**Priority:** P1 | **Effort:** S | **Type:** Migrate

## Problem Statement
`internal/llmclient/client.go` already classifies HTTP errors informally via the `retryableStatus` map and `HTTPStatusError` type, but this classification is not exposed through the `internal/errors` taxonomy. Callers cannot use `errors.IsRetryable` to decide whether to retry an llmclient error. The classification logic is duplicated and not reusable outside the package.

## Solution Overview
Wrap HTTP/transport errors returned by `internal/llmclient/client.go:send` in `internal/errors.ClassifiedError`:
- **Transient** (retryable): 429, 5xx, and transport-level failures (connection reset, EOF, DNS blip)
- **Permanent** (not retryable): other non-2xx (400, 401, 403, 404)

The wrapping preserves `errors.As` reachability to `*HTTPStatusError` so existing tests (`TestComplete_HTTPStatusErrorSurfacedForClassification`, `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries`) and callers continue to work unchanged.

## Technical Implementation
### Steps
1. In `internal/llmclient/client.go:send`:
   - When a retryable status is exhausted (line ~290): wrap the error in `errors.NewTransient(err)`.
   - When a non-retryable status is returned (line ~298): wrap in `errors.NewPermanent(err)`.
   - When a transport error occurs and retries are exhausted (line ~282): wrap in `errors.NewTransient(err)`.
   - Context cancellation errors: do NOT wrap (they are their own sentinel from `ctx.Err()`).
2. The `HTTPStatusError` type remains unchanged. The `ClassifiedError` wraps it, so `errors.As(err, &httpErr)` still reaches the `HTTPStatusError` through the wrapper via `Unwrap()`.
3. Import `internal/errors` in `internal/llmclient/client.go`.
4. Run the full test suite to verify `errors.As` contracts are preserved.

## Files to Create/Modify
- `internal/llmclient/client.go` — modify (wrap errors in `ClassifiedError` in `send`)
- `internal/llmclient/client_test.go` — verify existing tests still pass; add classification assertions

## Documentation Links
- [Error Classification System](../documentation/error-classification-system.md)

## Related Files (from codebase-discovery.json)
- `internal/llmclient/client.go:send` (line 253) — retry loop where errors are returned
- `internal/llmclient/client.go:retryableStatus` (line 37) — defines which statuses are transient
- `internal/llmclient/client.go:HTTPStatusError` (line 307) — must remain reachable via `errors.As`
- `internal/llmclient/client_test.go:TestComplete_HTTPStatusErrorSurfacedForClassification` (line 437) — must not break
- `internal/llmclient/client_test.go:TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries` (line 459) — must not break
- `internal/errors/errors.go` — `ClassifiedError`, `NewTransient`, `NewPermanent`, `IsRetryable`

## Success Criteria
- [ ] Retryable status errors (429, 5xx) are wrapped in `errors.NewTransient`
- [ ] Non-retryable status errors (400, 401, 403, 404) are wrapped in `errors.NewPermanent`
- [ ] Transport-level errors (exhausted retries) are wrapped in `errors.NewTransient`
- [ ] `errors.IsRetryable(err)` returns `true` for 429/5xx/transport errors
- [ ] `errors.IsRetryable(err)` returns `false` for 400/401/403/404 errors
- [ ] `errors.As(err, &httpStatusErr)` still reaches `*HTTPStatusError` through the wrapper
- [ ] `errors.Is(err, context.DeadlineExceeded)` still works for timeout errors
- [ ] `TestComplete_HTTPStatusErrorSurfacedForClassification` passes unchanged
- [ ] `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries` passes unchanged
- [ ] `go test ./internal/llmclient/...` passes

## Manual Code Review
- [ ] Codebase has been reviewed

## Test Strategy
**Unit Tests:**
- `TestComplete_TransientError_IsRetryable` — 503 response exhausted retries, assert `errors.IsRetryable(err)` returns `true`
- `TestComplete_PermanentError_NotRetryable` — 404 response, assert `errors.IsRetryable(err)` returns `false`
- `TestComplete_TransientError_ErrorsAsHTTPStatusError` — 503 response, assert `errors.As(err, &httpErr)` succeeds and `httpErr.Status == 503`
- `TestComplete_PermanentError_ErrorsAsHTTPStatusError` — 401 response, assert `errors.As(err, &httpErr)` succeeds and `httpErr.Status == 401`
- `TestComplete_TransportError_IsRetryable` — transport failure (server down), assert `errors.IsRetryable(err)` returns `true`
- `TestComplete_ContextDeadline_NotWrapped` — context with past deadline, assert error is `context.DeadlineExceeded` (not wrapped in ClassifiedError)

**Test Files:**
- `internal/llmclient/client_test.go` (modify)

## Risk Mitigation
- **errors.As preservation**: `ClassifiedError.Unwrap()` returns the inner error, so `errors.As` reaches through to `*HTTPStatusError`. This is validated by the existing tests — if they pass, the contract is preserved.
- **Nil error safety**: `errors.NewTransient(nil)` returns nil (per Task 05's nil-safety design), so code paths that return `nil` error are unaffected.
- **No double-wrapping**: The `send` function has clear return points. Each error is wrapped exactly once at the point of return.
- **Test breakage risk (from plan)**: Medium severity. Mitigated by running the full test suite and verifying `errors.As` contracts.

## Dependencies
- Task 05 (error-classification) — `internal/errors` package with `ClassifiedError`, `NewTransient`, `NewPermanent`, `IsRetryable`

## Definition of Done
- [ ] HTTP errors in `send` are wrapped in `ClassifiedError` with correct classification
- [ ] `errors.IsRetryable` returns correct values for all error types
- [ ] `errors.As` reaches `*HTTPStatusError` through the wrapper
- [ ] All existing llmclient tests pass unchanged
- [ ] New classification tests added
- [ ] `go test ./internal/llmclient/...` passes
- [ ] `go test ./...` passes (no regressions)

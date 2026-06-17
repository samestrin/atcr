# Task 05: Error Classification System (internal/errors)

**Source:** Plan 4.0 – Debt Item #5
**Priority:** P1 | **Effort:** S | **Type:** Add

## Problem Statement

Errors are wrapped with `%w` in some places (`internal/llmclient`) but not consistently. There is no taxonomy distinguishing transient failures (retryable) from permanent failures (user error, config issue) from system failures (panic, bug). Every error site reinvents severity. The only existing classification lives informally inside `internal/llmclient/client.go` as a `retryableStatus` map and `HTTPStatusError` type — these need to be formalized into a reusable package.

## Solution Overview

Create `internal/errors/errors.go` — a standalone package providing a `ClassifiedError` type that wraps any error with a `Classification` label and a `Retryable` flag. Four constructors (`NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError`) cover all failure modes. `IsRetryable` provides a single-question retryability check. The type implements `Error()` and `Unwrap()` so `errors.As` and `errors.Is` reach through to the underlying error, preserving the contract that existing `llmclient` callers and tests depend on.

## Technical Implementation

### Steps

1. **Create `internal/errors/errors.go`** with the following components:

   - `Classification` type (`type Classification string`) with four constants:
     - `Transient Classification = "transient"` — retryable (network, 429, 5xx)
     - `Permanent Classification = "permanent"` — not retryable (401, 403, 404)
     - `UserError Classification = "user_error"` — bad input, config issue
     - `SystemError Classification = "system_error"` — bug, panic, unexpected

   - `ClassifiedError` struct:
     ```go
     type ClassifiedError struct {
         Err            error
         Classification Classification
         Retryable      bool
     }
     ```

   - `Error()` method: delegates to `e.Err.Error()`
   - `Unwrap()` method: returns `e.Err`

   - Four constructors:
     - `NewTransient(err error) error` — returns `&ClassifiedError{Err: err, Classification: Transient, Retryable: true}`
     - `NewPermanent(err error) error` — returns `&ClassifiedError{Err: err, Classification: Permanent, Retryable: false}`
     - `NewUserError(err error) error` — returns `&ClassifiedError{Err: err, Classification: UserError, Retryable: false}`
     - `NewSystemError(err error) error` — returns `&ClassifiedError{Err: err, Classification: SystemError, Retryable: false}`

   - `IsRetryable(err error) bool` function — uses `errors.As` to find a `*ClassifiedError` in the chain and returns its `Retryable` field; returns `false` if no `ClassifiedError` is found

2. **Handle nil error input in constructors**: If `err` is nil, constructors return nil (no wrapper needed). This prevents `nil` errors from being wrapped and surfacing as non-nil interface values.

3. **Create `internal/errors/errors_test.go`** with comprehensive tests (see Test Strategy below).

## Files to Create/Modify

- `internal/errors/errors.go` — create
- `internal/errors/errors_test.go` — create

## Documentation Links

- [Error Classification System](../documentation/error-classification-system.md)

## Related Files (from codebase-discovery.json)

- `internal/llmclient/client.go:37-45` — existing `retryableStatus` map (429, 500, 502, 503, 504)
- `internal/llmclient/client.go:307-330` — `HTTPStatusError` type to preserve `errors.As` contract for
- `internal/llmclient/client.go:253-305` — `send` function with retry/backoff loop
- `internal/llmclient/client_test.go:437-457` — `TestComplete_HTTPStatusErrorSurfacedForClassification` locks in `errors.As` contract
- `internal/llmclient/client_test.go:459-475` — `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries` verifies unwrap through wrapper

## Success Criteria

- [ ] `ClassifiedError` implements `Error()` and `Unwrap()`
- [ ] `NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError` constructors exist
- [ ] `IsRetryable` returns `true` for `Transient`, `false` for `Permanent`/`UserError`/`SystemError`
- [ ] `errors.As` and `errors.Is` reach through `ClassifiedError` to underlying error
- [ ] Classification constants have correct string values (`"transient"`, `"permanent"`, `"user_error"`, `"system_error"`)
- [ ] Constructors return `nil` when passed a `nil` error
- [ ] All tests pass (`go test ./internal/errors/...`)

## Manual Code Review

- [ ] Codebase has been reviewed

## Test Strategy

**Unit Tests:**

- Classification constants have correct string values (`"transient"`, `"permanent"`, `"user_error"`, `"system_error"`)
- `NewTransient` sets `Classification: Transient` and `Retryable: true`
- `NewPermanent` sets `Classification: Permanent` and `Retryable: false`
- `NewUserError` sets `Classification: UserError` and `Retryable: false`
- `NewSystemError` sets `Classification: SystemError` and `Retryable: false`
- Each constructor returns `nil` when given a `nil` error
- `Error()` delegates to underlying error's `Error()`
- `Unwrap()` returns the underlying error
- `IsRetryable` returns `true` for `Transient`, `false` for all others
- `IsRetryable` returns `false` for a non-`ClassifiedError` error
- `errors.As` reaches through `ClassifiedError` to find a custom error type underneath
- `errors.Is` reaches through `ClassifiedError` to match a sentinel error
- `ClassifiedError` wrapping another `ClassifiedError` — `IsRetryable` finds the outermost classification

**Test Files:**

- `internal/errors/errors_test.go`

## Risk Mitigation

- **Standalone package**: `internal/errors` has zero dependencies on other ATCR packages, so it cannot break anything during creation. No existing code imports it.
- **Nil safety**: Constructors return `nil` for `nil` input, preventing the common Go pitfall of nil-error-wrapped-in-non-nil-interface.
- **Preserves existing contracts**: This task only creates the new package; the `llmclient` migration (Task 11) is where `errors.As` compatibility with `HTTPStatusError` must be verified.
- **Simple surface**: The entire public API is ~50 lines. Low complexity means low risk of subtle bugs.

## Dependencies

- None (standalone package)

## Definition of Done

- [ ] `internal/errors/errors.go` exists with `Classification` type, four constants, `ClassifiedError` struct, four constructors, `IsRetryable` function
- [ ] `internal/errors/errors_test.go` exists with full coverage of all success criteria
- [ ] All tests pass: `go test ./internal/errors/...`
- [ ] `go vet ./internal/errors/...` passes with no warnings
- [ ] No imports of other `internal/` packages (standalone)
- [ ] Code compiles with no unused imports or variables

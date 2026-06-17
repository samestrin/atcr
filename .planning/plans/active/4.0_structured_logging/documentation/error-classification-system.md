# Error Classification System (internal/errors)

**Priority:** CRITICAL

## Overview

The `internal/errors` package introduces a shared error taxonomy to ATCR, classifying failures as transient (retryable), permanent (non-retryable), user errors, or system errors. This consolidation eliminates ad-hoc error handling scattered across packages and provides a single source of truth for retryability decisions.

The only existing error classification in the codebase lives informally inside `internal/llmclient/client.go`, where a `retryableStatus` map and the `HTTPStatusError` type distinguish retryable (429, 5xx) from permanent HTTP failures. The new package formalizes this pattern into a reusable API while preserving the `errors.As` contract that existing callers and tests depend on.

The implementation is Phase 2 of the 4.0 structured logging plan. Phase 4 migrates `internal/llmclient/client.go` to wrap its HTTP errors in `ClassifiedError`. Neither `internal/errors` nor `internal/log` exists in the codebase today.

## Key Concepts

### Classification Categories

Four classification categories cover all failure modes:

| Classification | Value | Meaning |
|---|---|---|
| `Transient` | `"transient"` | Retryable: network errors, 429, 5xx |
| `Permanent` | `"permanent"` | Not retryable: 401, 403, 404 |
| `UserError` | `"user_error"` | Bad input or configuration issue |
| `SystemError` | `"system_error"` | Bug, panic, or unexpected failure |

> Source: [original-requirements.md:Error Taxonomy]

### ClassifiedError Type

`ClassifiedError` wraps an existing error with a classification label and a `Retryable` flag. It implements both `Error()` and `Unwrap()` so that `errors.As` and `errors.Is` continue to reach the underlying error through the wrapper.

> Source: [original-requirements.md:Error Taxonomy]

### Existing HTTP Error Classification

The `retryableStatus` map in `internal/llmclient/client.go` defines which HTTP statuses are worth retrying. This map drives the Transient vs Permanent distinction in the new taxonomy.

> Source: [internal/llmclient/client.go:retryableStatus, lines 37-45]

### HTTPStatusError Contract

The `HTTPStatusError` type (lines 307-330) is a non-2xx provider response surfaced to callers for classification via `errors.As`. It carries the HTTP `Status` code and a bounded, key-redacted `Snippet` of the provider's error body. The `send` function (line 253) returns this type for both retryable (429/5xx) and non-retryable failures. Any wrapper in `internal/errors` must preserve `errors.As` reachability so existing tests and callers keep working.

> Source: [internal/llmclient/client.go:HTTPStatusError, lines 307-330]
> Source: [internal/llmclient/client.go:send, lines 253-305]

### Existing Test Contract

The test `TestComplete_HTTPStatusErrorSurfacedForClassification` at `client_test.go:437` asserts that callers can classify failures by status via `errors.As`, that the `Status` field matches, and that the `Error()` text contract is preserved. The companion test at line 459 verifies `errors.As` unwraps through the `"exhausted retries: %w"` wrapper for retryable (503) failures.

> Source: [internal/llmclient/client_test.go:TestComplete_HTTPStatusErrorSurfacedForClassification, lines 437-457]

### Integration Risk

The plan identifies a medium-severity risk: "Error classification breaks existing error-matching tests." The mitigation is to run the full test suite after the llmclient migration and update tests to use `errors.Is` where needed.

> Source: [plan.md:Risk Mitigation]

## Code Examples

### Retryable Status Map (existing, verbatim)

> Source: [internal/llmclient/client.go:retryableStatus, lines 37-45]

```go
// retryableStatus is the set of HTTP statuses worth retrying. Every other
// non-2xx (e.g. 400, 401, 403, 404) fails immediately.
var retryableStatus = map[int]bool{
    http.StatusTooManyRequests:     true, // 429
    http.StatusInternalServerError: true, // 500
    http.StatusBadGateway:          true, // 502
    http.StatusServiceUnavailable:  true, // 503
    http.StatusGatewayTimeout:      true, // 504
}
```

### HTTPStatusError Type (existing, verbatim)

> Source: [internal/llmclient/client.go:HTTPStatusError, lines 307-330]

```go
// HTTPStatusError is a non-2xx provider response surfaced to callers so they
// can classify the failure by HTTP status (via errors.As) instead of parsing
// the message string. Snippet is the bounded, whitespace-collapsed,
// key-redacted prefix of the provider's error body (empty when none was sent).
// It survives the exhausted-retries wrapper, so errors.As reaches it for both
// retryable (429/5xx) and non-retryable (401/403/404) failures.
type HTTPStatusError struct {
    Status  int
    Snippet string
}

func (e *HTTPStatusError) Error() string {
    if e.Snippet == "" {
        return fmt.Sprintf("provider returned HTTP %d", e.Status)
    }
    return fmt.Sprintf("provider returned HTTP %d: %s", e.Status, e.Snippet)
}

func httpStatusError(status int, snippet string) error {
    return &HTTPStatusError{Status: status, Snippet: snippet}
}
```

### Planned Classification API (from original-requirements.md, verbatim)

> Source: [original-requirements.md:Error Taxonomy]

```go
// internal/errors/errors.go
package errors

// Classification categories
type Classification string

const (
    Transient   Classification = "transient"   // retryable (network, 429, 5xx)
    Permanent   Classification = "permanent"   // not retryable (401, 403, 404)
    UserError   Classification = "user_error"  // bad input, config issue
    SystemError Classification = "system_error" // bug, panic, unexpected
)

// ClassifiedError wraps an error with a classification.
type ClassifiedError struct {
    Err            error
    Classification Classification
    Retryable      bool
}

func (e *ClassifiedError) Error() string { return e.Err.Error() }
func (e *ClassifiedError) Unwrap() error { return e.Err }

// NewTransient wraps err as a transient, retryable error.
func NewTransient(err error) error

// NewPermanent wraps err as a permanent, non-retryable error.
func NewPermanent(err error) error

// NewUserError wraps err as a user/config error.
func NewUserError(err error) error

// NewSystemError wraps err as a system/bug error.
func NewSystemError(err error) error

// IsRetryable returns true if err is classified as transient.
func IsRetryable(err error) bool
```

### Existing Test Contract (verbatim)

> Source: [internal/llmclient/client_test.go:TestComplete_HTTPStatusErrorSurfacedForClassification, lines 437-457]

```go
func TestComplete_HTTPStatusErrorSurfacedForClassification(t *testing.T) {
    srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        w.WriteHeader(http.StatusNotFound)
        _, _ = io.WriteString(w, `{"error":{"message":"model not found"}}`)
    }))
    defer srv.Close()
    t.Setenv("TEST_KEY", testKey)

    _, err := fastRetry(srv.Client()).Complete(context.Background(), Invocation{
        BaseURL: srv.URL, APIKeyEnv: "TEST_KEY", Model: "m",
    })
    require.Error(t, err)

    var se *HTTPStatusError
    require.True(t, errors.As(err, &se), "callers must be able to classify by status via errors.As")
    assert.Equal(t, http.StatusNotFound, se.Status)
    assert.Contains(t, se.Snippet, "model not found")
    // Error() text contract preserved for existing callers.
    assert.Contains(t, err.Error(), "HTTP 404")
    assert.Contains(t, err.Error(), "model not found")
}
```

## Quick Reference

| Item | Location | Purpose |
|---|---|---|
| `retryableStatus` map | `internal/llmclient/client.go:37-45` | HTTP statuses worth retrying (429, 5xx) |
| `HTTPStatusError` type | `internal/llmclient/client.go:307-330` | Non-2xx response surfaced via `errors.As` |
| `send` function | `internal/llmclient/client.go:253-305` | Retry/backoff loop with transient classification |
| `TestComplete_HTTPStatusErrorSurfacedForClassification` | `internal/llmclient/client_test.go:437-457` | Locks in `errors.As` contract for status classification |
| `TestComplete_HTTPStatusErrorSurfacedThroughExhaustedRetries` | `internal/llmclient/client_test.go:459-475` | Verifies `errors.As` through `"exhausted retries: %w"` wrapper |
| Planned `internal/errors` | `internal/errors/errors.go` (not yet created) | Classification constructors: `NewTransient`, `NewPermanent`, `NewUserError`, `NewSystemError` |
| Plan phase 2 | `.planning/plans/active/4.0_structured_logging/plan.md:72` | Error taxonomy creation |
| Plan phase 4 | `.planning/plans/active/4.0_structured_logging/plan.md:76` | llmclient migration to `ClassifiedError` |
| Risk: test breakage | `.planning/plans/active/4.0_structured_logging/plan.md:109` | Medium severity; mitigate with full test suite run |

## Related Documentation

- [Structured Logging Plan](../plan.md) — full plan with all five phases
- [Core Logging Package (internal/log)](core-logging-package.md) — companion package for structured output
- [OpenAI Error Handling Reference](../../../specifications/packages/openai.md) — reference patterns from the OpenAI Go SDK (not a project dependency)

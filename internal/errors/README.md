# internal/errors

A small error-classification taxonomy for ATCR. It decides retryability in one
place instead of reinventing it at every error site. The package depends only on
the standard library.

## Classification

`Classification` labels the kind of failure an error represents:

| Constant | Value | Meaning | Retryable |
|----------|-------|---------|-----------|
| `Transient` | `"transient"` | Network error, 429, 5xx — worth retrying | yes |
| `Permanent` | `"permanent"` | 401, 403, 404 — retrying will not help | no |
| `UserError` | `"user_error"` | Bad input or configuration | no |
| `SystemError` | `"system_error"` | Bug, panic, or otherwise unexpected | no |

## ClassifiedError

`ClassifiedError` wraps an existing error with a `Classification` and a
`Retryable` flag. It implements `Error()` and `Unwrap()`, so `errors.Is` and
`errors.As` reach through the wrapper to the underlying error — callers and tests
that match the inner error keep working.

**Classification contract:** an error is classified exactly once, at the point it
is first recognized (for example, the llmclient maps an HTTP status to
`Transient` or `Permanent`). The constructors are not meant to re-wrap an
already-classified error. Do not escalate an inner `Permanent` failure by
re-wrapping it as `Transient`.

## Constructors

Each constructor wraps `err` with the matching classification. All of them are
nil-safe: they return a true `nil` interface for `nil` input (never a non-nil
interface wrapping a nil concrete value), so a possibly-nil error can be passed
through without tripping the Go nil-interface trap.

```go
func NewTransient(err error) error   // retryable
func NewPermanent(err error) error   // not retryable
func NewUserError(err error) error   // not retryable
func NewSystemError(err error) error // not retryable
```

## IsRetryable

`IsRetryable(err error) bool` reports whether `err` carries a transient
classification. It finds the **outermost** `*ClassifiedError` in the chain via
`errors.As` and returns its `Retryable` field; it returns `false` when no
`ClassifiedError` is present (including for a plain `errors.New(...)`).

## Example

```go
import (
    atcrerrors "github.com/samestrin/atcr/internal/errors"
)

func classify(status int, err error) error {
    switch {
    case status == 429 || status >= 500:
        return atcrerrors.NewTransient(err) // retry
    default:
        return atcrerrors.NewPermanent(err) // give up
    }
}

func handle(err error) {
    if atcrerrors.IsRetryable(err) {
        // back off and retry
        return
    }
    // surface a permanent failure
}
```

## See also

- [`docs/logging.md`](../../docs/logging.md) — structured logging; classified
  errors are surfaced through the shared log sink.

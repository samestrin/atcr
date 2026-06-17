// Package errors provides a small error-classification taxonomy for ATCR.
// It distinguishes transient (retryable) failures from permanent, user, and
// system failures so retryability is decided in one place instead of being
// reinvented at every error site. ClassifiedError wraps an existing error with
// a Classification label and a Retryable flag while preserving the errors.Is /
// errors.As chain via Unwrap, so callers and tests that match the underlying
// error keep working. The package depends only on the standard library.
package errors

import "errors"

// Classification labels the kind of failure an error represents.
type Classification string

const (
	// Transient marks a retryable failure (network error, 429, 5xx).
	Transient Classification = "transient"
	// Permanent marks a non-retryable failure (401, 403, 404).
	Permanent Classification = "permanent"
	// UserError marks a bad-input or configuration failure.
	UserError Classification = "user_error"
	// SystemError marks a bug, panic, or otherwise unexpected failure.
	SystemError Classification = "system_error"
)

// ClassifiedError wraps an error with a classification and a retryability flag.
// It implements Error and Unwrap so errors.Is and errors.As reach through the
// wrapper to the underlying error.
type ClassifiedError struct {
	Err            error
	Classification Classification
	Retryable      bool
}

// Error delegates to the underlying error's message.
func (e *ClassifiedError) Error() string { return e.Err.Error() }

// Unwrap returns the underlying error so errors.Is / errors.As can traverse it.
func (e *ClassifiedError) Unwrap() error { return e.Err }

// NewTransient wraps err as a transient, retryable error. It returns nil when
// err is nil, so a nil error never becomes a non-nil interface value.
func NewTransient(err error) error {
	if err == nil {
		return nil
	}
	return &ClassifiedError{Err: err, Classification: Transient, Retryable: true}
}

// NewPermanent wraps err as a permanent, non-retryable error. Returns nil for
// nil input.
func NewPermanent(err error) error {
	if err == nil {
		return nil
	}
	return &ClassifiedError{Err: err, Classification: Permanent, Retryable: false}
}

// NewUserError wraps err as a user/config error. Returns nil for nil input.
func NewUserError(err error) error {
	if err == nil {
		return nil
	}
	return &ClassifiedError{Err: err, Classification: UserError, Retryable: false}
}

// NewSystemError wraps err as a system/bug error. Returns nil for nil input.
func NewSystemError(err error) error {
	if err == nil {
		return nil
	}
	return &ClassifiedError{Err: err, Classification: SystemError, Retryable: false}
}

// IsRetryable reports whether err carries a transient classification. It finds
// the outermost *ClassifiedError in the chain via errors.As and returns its
// Retryable field; it returns false when no ClassifiedError is present.
func IsRetryable(err error) bool {
	var ce *ClassifiedError
	if errors.As(err, &ce) {
		return ce.Retryable
	}
	return false
}

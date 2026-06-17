package errors_test

import (
	stderrors "errors"
	"fmt"
	"testing"

	apperrors "github.com/samestrin/atcr/internal/errors"
)

// TestClassificationConstants locks in the wire/string values of each
// classification — downstream consumers and logs match on these literals.
func TestClassificationConstants(t *testing.T) {
	cases := []struct {
		got  apperrors.Classification
		want string
	}{
		{apperrors.Transient, "transient"},
		{apperrors.Permanent, "permanent"},
		{apperrors.UserError, "user_error"},
		{apperrors.SystemError, "system_error"},
	}
	for _, c := range cases {
		if string(c.got) != c.want {
			t.Errorf("classification = %q, want %q", c.got, c.want)
		}
	}
}

// TestConstructors_SetClassificationAndRetryable verifies each constructor
// applies the correct Classification and Retryable flag.
func TestConstructors_SetClassificationAndRetryable(t *testing.T) {
	inner := stderrors.New("boom")
	cases := []struct {
		name           string
		ctor           func(error) error
		classification apperrors.Classification
		retryable      bool
	}{
		{"transient", apperrors.NewTransient, apperrors.Transient, true},
		{"permanent", apperrors.NewPermanent, apperrors.Permanent, false},
		{"user", apperrors.NewUserError, apperrors.UserError, false},
		{"system", apperrors.NewSystemError, apperrors.SystemError, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.ctor(inner)
			var ce *apperrors.ClassifiedError
			if !stderrors.As(err, &ce) {
				t.Fatalf("%s: errors.As did not find *ClassifiedError", c.name)
			}
			if ce.Classification != c.classification {
				t.Errorf("classification = %q, want %q", ce.Classification, c.classification)
			}
			if ce.Retryable != c.retryable {
				t.Errorf("retryable = %v, want %v", ce.Retryable, c.retryable)
			}
			if ce.Err != inner {
				t.Errorf("Err = %v, want %v", ce.Err, inner)
			}
		})
	}
}

// TestConstructors_NilInputReturnsNil verifies the nil-interface trap is
// avoided: a nil error in yields a true nil interface out.
func TestConstructors_NilInputReturnsNil(t *testing.T) {
	cases := []struct {
		name string
		ctor func(error) error
	}{
		{"transient", apperrors.NewTransient},
		{"permanent", apperrors.NewPermanent},
		{"user", apperrors.NewUserError},
		{"system", apperrors.NewSystemError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.ctor(nil)
			if err != nil {
				t.Errorf("%s(nil) = %v, want nil interface", c.name, err)
			}
		})
	}
}

// TestError_DelegatesToUnderlying verifies Error() returns the wrapped
// message verbatim.
func TestError_DelegatesToUnderlying(t *testing.T) {
	inner := stderrors.New("underlying message")
	err := apperrors.NewTransient(inner)
	if err.Error() != "underlying message" {
		t.Errorf("Error() = %q, want %q", err.Error(), "underlying message")
	}
}

// TestError_NilErrDoesNotPanic verifies a directly-constructed ClassifiedError
// with a nil Err falls back to the classification label instead of panicking.
func TestError_NilErrDoesNotPanic(t *testing.T) {
	ce := &apperrors.ClassifiedError{Classification: apperrors.SystemError}
	want := "classified error (system_error) with nil cause"
	if got := ce.Error(); got != want {
		t.Errorf("Error() with nil Err = %q, want %q", got, want)
	}
	if ce.Unwrap() != nil {
		t.Error("Unwrap() with nil Err should return nil to terminate the chain")
	}
}

// TestUnwrap_ReturnsUnderlying verifies Unwrap returns the wrapped error.
func TestUnwrap_ReturnsUnderlying(t *testing.T) {
	inner := stderrors.New("inner")
	ce := &apperrors.ClassifiedError{Err: inner, Classification: apperrors.Permanent}
	if got := ce.Unwrap(); got != inner {
		t.Errorf("Unwrap() = %v, want %v", got, inner)
	}
}

// TestIsRetryable verifies retryability resolution across classifications and
// non-classified errors.
func TestIsRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"transient", apperrors.NewTransient(stderrors.New("x")), true},
		{"permanent", apperrors.NewPermanent(stderrors.New("x")), false},
		{"user", apperrors.NewUserError(stderrors.New("x")), false},
		{"system", apperrors.NewSystemError(stderrors.New("x")), false},
		{"plain", stderrors.New("unclassified"), false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := apperrors.IsRetryable(c.err); got != c.want {
				t.Errorf("IsRetryable(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// TestIsRetryable_WrappedDeepInChain verifies errors.As traversal still finds
// a ClassifiedError nested under fmt.Errorf wrappers.
func TestIsRetryable_WrappedDeepInChain(t *testing.T) {
	base := apperrors.NewTransient(stderrors.New("rate limited"))
	wrapped := fmt.Errorf("exhausted retries: %w", base)
	doubleWrapped := fmt.Errorf("review failed: %w", wrapped)
	if !apperrors.IsRetryable(doubleWrapped) {
		t.Error("IsRetryable did not reach ClassifiedError through fmt.Errorf wrappers")
	}
}

// sentinel is used to confirm errors.Is reaches through the wrapper.
var sentinel = stderrors.New("sentinel")

// TestErrorsIs_ReachesThroughWrapper verifies errors.Is matches a sentinel
// wrapped by a ClassifiedError.
func TestErrorsIs_ReachesThroughWrapper(t *testing.T) {
	err := apperrors.NewPermanent(sentinel)
	if !stderrors.Is(err, sentinel) {
		t.Error("errors.Is did not reach sentinel through ClassifiedError")
	}
}

// customError is a concrete error type used to confirm errors.As reaches the
// underlying type through the wrapper.
type customError struct{ code int }

func (c *customError) Error() string { return fmt.Sprintf("custom %d", c.code) }

// TestErrorsAs_ReachesUnderlyingType verifies errors.As finds a custom type
// beneath a ClassifiedError.
func TestErrorsAs_ReachesUnderlyingType(t *testing.T) {
	err := apperrors.NewSystemError(&customError{code: 42})
	var ce *customError
	if !stderrors.As(err, &ce) {
		t.Fatal("errors.As did not reach *customError through ClassifiedError")
	}
	if ce.code != 42 {
		t.Errorf("code = %d, want 42", ce.code)
	}
}

// TestIsRetryable_DoubleWrappedOutermostWins verifies that when a
// ClassifiedError wraps another ClassifiedError, errors.As stops at the
// outermost one, so its Retryable flag decides.
func TestIsRetryable_DoubleWrappedOutermostWins(t *testing.T) {
	innerTransient := apperrors.NewTransient(stderrors.New("inner"))
	// Outer is Permanent (not retryable); it must win over the inner transient.
	outerPermanent := apperrors.NewPermanent(innerTransient)
	if apperrors.IsRetryable(outerPermanent) {
		t.Error("outermost Permanent classification should win; IsRetryable should be false")
	}

	// Reverse: outer transient wins over inner permanent.
	innerPermanent := apperrors.NewPermanent(stderrors.New("inner"))
	outerTransient := apperrors.NewTransient(innerPermanent)
	if !apperrors.IsRetryable(outerTransient) {
		t.Error("outermost Transient classification should win; IsRetryable should be true")
	}
}

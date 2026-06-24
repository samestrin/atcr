package reconcile

import (
	"reflect"
	"strings"
	"testing"
)

// Minimal stdlib-only test assertion helpers. The reconcile module is a
// zero-dependency library: `go mod tidy ./reconcile` must yield an empty require
// block (a hard sprint success criterion and the core embeddability moat), so the
// library's own tests use only the standard `testing` package rather than testify
// (testify stays in the ATCR-internal test suite, which is not dependency-bound).

func eq[T comparable](t *testing.T, got, want T, msg string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func deepEq(t *testing.T, got, want any, msg string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: got %v, want %v", msg, got, want)
	}
}

func isTrue(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatalf("%s: expected true", msg)
	}
}

func length[T any](t *testing.T, s []T, want int, msg string) {
	t.Helper()
	if len(s) != want {
		t.Fatalf("%s: got len %d, want %d", msg, len(s), want)
	}
}

func contains(t *testing.T, s, sub, msg string) {
	t.Helper()
	if !strings.Contains(s, sub) {
		t.Fatalf("%s: %q does not contain %q", msg, s, sub)
	}
}

func hasPrefix(t *testing.T, s, prefix, msg string) {
	t.Helper()
	if !strings.HasPrefix(s, prefix) {
		t.Fatalf("%s: %q has no prefix %q", msg, s, prefix)
	}
}

func inDelta(t *testing.T, got, want, delta float64, msg string) {
	t.Helper()
	d := got - want
	if d < 0 {
		d = -d
	}
	if d > delta {
		t.Fatalf("%s: got %v, want within %v of %v", msg, got, delta, want)
	}
}

func notEq[T comparable](t *testing.T, a, b T, msg string) {
	t.Helper()
	if a == b {
		t.Fatalf("%s: both values equal %v", msg, a)
	}
}

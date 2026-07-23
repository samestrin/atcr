package astgroup

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSharedHost_IsSingleton verifies SharedHost returns one process-lifetime
// instance so the compiled-parser cache is amortized across reconciles instead of
// rebuilt per RunReconcile call.
func TestSharedHost_IsSingleton(t *testing.T) {
	h1 := SharedHost()
	h2 := SharedHost()
	require.NotNil(t, h1)
	require.Same(t, h1, h2, "SharedHost must return a single process-lifetime instance")
}

// TestGrouper_CloseDoesNotCloseHost verifies a Grouper borrows its Host rather
// than owning it: closing the per-run Grouper releases only its file-tree cache,
// leaving the shared Host usable for the next reconcile.
func TestGrouper_CloseDoesNotCloseHost(t *testing.T) {
	h := NewHost()
	defer func() { _ = h.Close() }()

	g := NewGrouper(t.TempDir(), h)
	require.NoError(t, g.Close())

	// The borrowed Host must remain open and usable after the Grouper is closed.
	_, err := h.Parser("go")
	require.NoError(t, err, "Grouper.Close must not close the borrowed Host")
}

// TestGrouper_CloseClosesOwnedHost is the failure-path complement to
// TestGrouper_CloseDoesNotCloseHost: a Grouper that OWNS its Host (the no-host
// NewGrouper path sets ownsHost) must close that Host on Close, and Close must
// stay idempotent. This exercises the ownsHost branch of Grouper.Close, which the
// borrowed-host test never reaches — closing an owned Host is where Close can
// surface the underlying runtime's error.
func TestGrouper_CloseClosesOwnedHost(t *testing.T) {
	h := NewHost()
	g := &Grouper{host: h, ownsHost: true}

	require.NoError(t, g.Close(), "closing an owned Grouper must succeed")

	// The owned Host must be closed once its Grouper closes.
	_, err := h.Parser("go")
	require.Error(t, err, "Grouper.Close must close the Host it owns")

	require.NoError(t, g.Close(), "a second Close on an owned Grouper must be idempotent")
}

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

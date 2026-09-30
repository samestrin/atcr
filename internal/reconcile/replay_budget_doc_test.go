package reconcile

import (
	"fmt"
	"os"
	"testing"

	"github.com/samestrin/atcr/internal/payload"
	"github.com/stretchr/testify/require"
)

// docs/registry.md states the replayed-reasoning reserve as a number; it must be
// the number review and doctor actually reserve.
func TestRegistryDocStatesTheReplayReserve(t *testing.T) {
	doc, err := os.ReadFile("../../docs/registry.md")
	require.NoError(t, err)
	require.Contains(t, string(doc),
		fmt.Sprintf("plus %d extra output caps for reasoning", payload.ReasoningReplayReserveCaps))
	require.Contains(t, string(doc), "`reasoning_replay_bytes`")
}

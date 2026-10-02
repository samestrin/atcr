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

	// And it must state the right EXEMPTION. The trip keys on the resolved sizing
	// (loop.go: ResolvedWindow and ResolvedMaxTokens both non-zero), not on a funded
	// byte budget — so "an agent with no funded budget is not tripped" named the
	// opposite case: a sized agent whose reserve closed its byte budget records
	// EffectiveBudget 0 and still trips (TD docs/registry.md:270).
	require.Contains(t, string(doc), "no sizing record (no resolved window or output cap) is not tripped",
		"the exemption must be stated as the absence of SIZING")
	require.Contains(t, string(doc), "whose reserve closed its byte budget still trips",
		"and the doc must say the funded-budget case is NOT the exemption")
	require.NotContains(t, string(doc), "no sizing record (no funded budget)",
		"the superseded wording named the wrong case")
}

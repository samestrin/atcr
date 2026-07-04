package reconcile

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestJSONFinding_JustificationField verifies the additive justification field
// (Epic 18.2): present under the "justification" key when set, and omitted
// entirely when empty so findings.json stays byte-identical to pre-18.2 output.
func TestJSONFinding_JustificationField(t *testing.T) {
	b, err := json.Marshal(JSONFinding{Justification: "narrative context from review.md"})
	require.NoError(t, err)
	require.Contains(t, string(b), `"justification":"narrative context from review.md"`)

	b2, err := json.Marshal(JSONFinding{})
	require.NoError(t, err)
	require.NotContains(t, string(b2), "justification")
}

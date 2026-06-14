package fanout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AC 02-04 S1 / 01-06 S2: a tool agent's counters and tripped budgets serialize.
func TestStatusFor_ToolAgentSerializesCounters(t *testing.T) {
	r := Result{
		Agent: "a", Status: StatusOK, Tools: true,
		Turns: 4, ToolCalls: 6, ToolBytes: 2500,
		TrippedBudgets: []string{budgetMaxTurns},
	}
	data, err := json.Marshal(statusFor(r, 0))
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, `"turns":4`)
	assert.Contains(t, s, `"tool_calls":6`)
	assert.Contains(t, s, `"tool_bytes":2500`)
	assert.Contains(t, s, `"tripped_budgets":["max_turns"]`)
	assert.NotContains(t, s, `"tools_degraded"`, "false degraded flag is omitted")
}

// AC 02-04 EC2 / 01-06 EC2: a non-tool agent's status.json is unchanged from 1.x
// (no tool fields at all).
func TestStatusFor_NonToolAgentOmitsAllToolFields(t *testing.T) {
	r := Result{Agent: "a", Status: StatusOK} // Tools:false
	data, err := json.Marshal(statusFor(r, 0))
	require.NoError(t, err)
	s := string(data)
	for _, f := range []string{"turns", "tool_calls", "tool_bytes", "tripped_budgets", "tools_degraded"} {
		assert.NotContainsf(t, s, `"`+f+`"`, "non-tool status.json must omit %q", f)
	}
}

// AC 02-04 EC3: the degrade path records explicit zero counters and tools_degraded.
func TestStatusFor_DegradePathExplicitZeros(t *testing.T) {
	r := Result{Agent: "a", Status: StatusOK, Tools: true, ToolsDegraded: true}
	data, err := json.Marshal(statusFor(r, 0))
	require.NoError(t, err)
	s := string(data)
	assert.Contains(t, s, `"turns":0`)
	assert.Contains(t, s, `"tool_calls":0`)
	assert.Contains(t, s, `"tool_bytes":0`)
	assert.Contains(t, s, `"tools_degraded":true`)
}

// AC 02-04 / 06-03: WriteStatus persists the tool fields and they round-trip.
func TestWriteStatus_ToolFieldsRoundTrip(t *testing.T) {
	r := Result{
		Agent: "a", Status: StatusTimeout, Tools: true,
		Turns: 2, ToolCalls: 3, ToolBytes: 1200,
		TrippedBudgets: []string{budgetToolBytes, budgetTimeout},
	}
	st := statusFor(r, 0)
	path := filepath.Join(t.TempDir(), "status.json")
	require.NoError(t, WriteStatus(path, &st))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var got AgentStatus
	require.NoError(t, json.Unmarshal(raw, &got))
	require.NotNil(t, got.Turns)
	assert.Equal(t, 2, *got.Turns)
	require.NotNil(t, got.ToolCalls)
	assert.Equal(t, 3, *got.ToolCalls)
	require.NotNil(t, got.ToolBytes)
	assert.EqualValues(t, 1200, *got.ToolBytes)
	assert.Equal(t, []string{budgetToolBytes, budgetTimeout}, got.TrippedBudgets)
}

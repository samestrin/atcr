package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A probe that never placed a call reports max_tokens 0, and 0 must SERIALIZE.
//
// probe() short-circuits on invalid_config and missing_key before the budget is
// resolved, so pr.maxTokens stays 0 on those rows — and cli/doctor.go rejects
// --max-tokens <= 0, so 0 can never mean "no cap". Under omitempty the key simply
// vanishes, and a consumer cannot tell "no call was made" from "uncapped" or from a
// report written by an atcr that predates the field.
//
// This is the convention the sibling tallies in this repo already state outright:
// PoolSummary.TruncatedZeroFindings and FallbackCount are deliberately NOT omitempty
// "so a 0 is distinguishable from an older summary.json that predates the field"
// (internal/fanout/artifacts.go). The same reasoning governs here.
func TestRenderJSON_MaxTokensSerializesZeroForAProbeThatNeverRan(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{
		Agent:    "a",
		Provider: "p",
		Model:    "m",
		Status:   StatusMissingKey, // short-circuits before the budget is resolved
	}}}))
	out := buf.String()

	assert.Contains(t, out, `"max_tokens"`,
		"a 0 cap means the probe never placed a call; omitting the key makes that "+
			"indistinguishable from an uncapped probe or an older report")

	// And it must round-trip as an explicit 0, not as an absent field.
	var parsed struct {
		Agents []map[string]any `json:"agents"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &parsed))
	require.Len(t, parsed.Agents, 1)
	v, present := parsed.Agents[0]["max_tokens"]
	require.True(t, present, "the key must be present")
	assert.Equal(t, float64(0), v)
}

// MaxTokensSource's doc promises it is empty when no call was placed (MaxTokens 0), but
// the tier was resolved before the budget was known to be positive — so a caller passing
// MaxTokensSet with a non-positive cap got MaxTokens 0 alongside a non-empty source, a
// pair the field's own contract says cannot exist. The CLI rejects that input, but Run is
// exported and the invariant should hold for any caller rather than resting on a
// precondition enforced one layer up.
func TestRun_MaxTokensSourceIsEmptyWhenNoCapWasApplied(t *testing.T) {
	t.Setenv("ATCR_DOCTOR_KEY", "k")
	fake := newFake(func(inv llmclient.Invocation) (string, error) { return Marker(testNonce), nil })

	rep := Run(context.Background(), fake, twoAgentSharedTarget(t),
		Options{Nonce: testNonce, MaxTokens: 0, MaxTokensSet: true})

	require.NotEmpty(t, rep.Agents)
	assert.Equal(t, 0, rep.Agents[0].MaxTokens, "precondition: no cap was applied")
	assert.Empty(t, rep.Agents[0].MaxTokensSource,
		"a source without a cap is the state the field's doc says cannot occur")
}

// The healthy case keeps reporting the resolved cap — dropping omitempty must not
// change what a normal row says.
func TestRenderJSON_MaxTokensStillReportsAResolvedCap(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, RenderJSON(&buf, &Report{Agents: []AgentResult{{
		Agent: "a", Provider: "p", Model: "m", Status: StatusOK, MaxTokens: 32000,
	}}}))
	assert.True(t, strings.Contains(buf.String(), `"max_tokens": 32000`),
		"a resolved cap is still reported verbatim")
}

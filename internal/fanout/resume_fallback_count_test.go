package fanout

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PoolSummary.FallbackCount is deliberately NOT omitempty so a published 0 reads as a
// MEASUREMENT rather than as an older summary.json that predates the field — its own
// contract says so (artifacts.go), docs/registry.md restates it, and
// doctor.AgentResult.MaxTokens cites it as settled precedent for dropping omitempty.
//
// RebuildPool is the sole writer of summary.json on the resume path, and it never tallied
// FallbackUsed — so a resumed run published "fallback_count": 0 beside
// agents[].fallback_used: true in the SAME file, which is precisely the ambiguity the
// non-omitempty tag was chosen to prevent.
//
// The asymmetry is new: before the truncated-to-zero work RebuildPool computed neither
// tally, so the two documented-as-siblings degraded identically. TruncatedZeroFindings
// was added to the rebuild and FallbackCount was left behind.
func TestRebuildPool_RecoversFallbackCount(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "greta", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e", FallbackUsed: true, FallbackFrom: "greta-primary"},
		{Agent: "kai", Status: StatusOK, Content: "HIGH|y.go:2|p|f|correctness|5|e", FallbackUsed: true, FallbackFrom: "kai-primary"},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|z.go:3|p|f|correctness|5|e"},
	}, nil))

	_, statuses, err := RebuildPool(context.Background(), poolDir, []string{"greta", "kai", "bruce"})
	require.NoError(t, err)

	data, rerr := os.ReadFile(filepath.Join(poolDir, summaryFile))
	require.NoError(t, rerr)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))

	// Count the per-agent truth from the same file, so the assertion is that the two
	// halves of ONE artifact agree — not that a literal matches a literal.
	perAgent := 0
	for _, a := range ps.Agents {
		if a.FallbackUsed {
			perAgent++
		}
	}
	require.Equal(t, 2, perAgent, "precondition: the rebuilt statuses carry the substitutions")

	assert.Equal(t, perAgent, ps.FallbackCount,
		"fallback_count is published as a measurement (never omitempty), so a resumed run "+
			"reporting 0 beside two agents[].fallback_used:true states two contradictory "+
			"things about the same run in the same file")

	returned := 0
	for _, st := range statuses {
		if st.FallbackUsed {
			returned++
		}
	}
	assert.Equal(t, perAgent, returned, "the returned statuses must agree with the written ones")
}

// A run with no substitution still publishes 0 — that is the measurement, and the reason
// the field is not omitempty. The tally must not become "present only when non-zero".
func TestRebuildPool_FallbackCountIsZeroWhenNothingFellBack(t *testing.T) {
	poolDir := filepath.Join(t.TempDir(), "sources", "pool")
	require.NoError(t, writeResumedAgents(poolDir, []Result{
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|z.go:3|p|f|correctness|5|e"},
	}, nil))

	_, _, err := RebuildPool(context.Background(), poolDir, []string{"bruce"})
	require.NoError(t, err)

	data, rerr := os.ReadFile(filepath.Join(poolDir, summaryFile))
	require.NoError(t, rerr)
	assert.Contains(t, string(data), `"fallback_count": 0`,
		"a 0 must be published, not omitted — that is what distinguishes 'no substitution' "+
			"from 'written before the field existed'")
}

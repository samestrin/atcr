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

// TD internal/fanout/artifacts.go:395 — Result.Salvaged reached NO artifact, so a
// reviewer that lost its whole contribution to a salvage was byte-identical in
// status.json to one that emitted garbled prose. These pin the three disclosures:
// the per-agent marker, the run-level tally, and the per-bin indices for a chunked
// persona.
func TestWritePool_RecordsSalvagedRepliesPerAgentAndInTheTally(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	results := []Result{
		// Salvaged: the client promoted reasoning into Content, so parseFindings
		// refuses it and the agent's zero findings are a refusal.
		{Agent: "drafter", Status: StatusOK, Salvaged: true, UnparseableResponse: true,
			Content: "HIGH|a.go:1|draft I never committed to|f|correctness|5|e"},
		{Agent: "clean", Status: StatusOK, Content: "HIGH|b.go:2|b|f|correctness|5|e|clean"},
	}
	_, err := WritePool(pool, results, nil)
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(pool, "summary.json"))
	require.NoError(t, err)
	var ps PoolSummary
	require.NoError(t, json.Unmarshal(data, &ps))

	assert.Equal(t, 1, ps.SalvagedCount, "the run-level tally must count the salvaged agent")
	byAgent := map[string]AgentStatus{}
	for _, a := range ps.Agents {
		byAgent[a.Agent] = a
	}
	assert.True(t, byAgent["drafter"].Salvaged, "the per-agent marker must say the reply was salvaged")
	assert.Equal(t, 0, byAgent["drafter"].FindingsCount, "precondition: the salvaged reply contributed nothing")
	assert.False(t, byAgent["clean"].Salvaged)

	// The key is always present, so a 0 is distinguishable from an older summary.
	assert.Contains(t, string(data), `"salvaged_count"`)
}

// A merged persona's per-bin indices: the persona-wide bit cannot say WHICH bin
// salvaged, and parseFindings refuses per bin.
func TestStatusFor_MergedPersonaNamesTheSalvagedBins(t *testing.T) {
	merged := mergeResultGroup([]Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft|f|correctness|5|e",
			Salvaged: true, UnparseableResponse: true},
	}, nil)
	require.Equal(t, []bool{false, true}, merged.chunkSalvaged, "precondition: the flags are aligned")

	st := statusFor(merged, findingsFor(merged, nil))
	assert.True(t, st.Salvaged, "the persona record must disclose the salvage")
	assert.Equal(t, []int{1}, st.SalvagedChunks, "the index of the salvaged bin must be named")
}

// The operator-facing half: a salvage must reach the console, not only an artifact
// an operator has to go looking for.
func TestWritePool_WarnsAboutSalvagedReplies(t *testing.T) {
	pool := filepath.Join(t.TempDir(), "pool")
	var err error
	out := captureWarnLog(t, func(ctx context.Context) {
		_, err = writePool(ctx, pool, []Result{
			{Agent: "drafter", Status: StatusOK, Salvaged: true, Content: "HIGH|a.go:1|draft|f|correctness|5|e"},
			{Agent: "bruce", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e"},
		}, nil, "")
	})
	require.NoError(t, err)
	assert.Contains(t, out, "salvaged", "a refused contribution must reach the console, not just summary.json")
	assert.Contains(t, out, "drafter", "the warning must name the agent whose contribution was refused")
	assert.NotContains(t, out, "bruce", "a clean agent is not a salvage")
}

// A chunked persona's Salvaged bit is an OR-fold over its bins, so one refused bin
// sets it beside a sibling bin that landed real findings. The disclosure must not
// then claim the reviewer contributed nothing — that is false of exactly the case the
// per-bin refusal exists to protect.
func TestWritePool_SalvagedBinBesideRealFindingsIsNotReportedAsTotalLoss(t *testing.T) {
	merged := mergeResultGroup([]Result{
		{Agent: "bruce", Status: StatusOK, Content: "MEDIUM|b.go:2|real finding|f|correctness|2|e"},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|a.go:1|draft|f|correctness|5|e", Salvaged: true},
	}, nil)
	pool := filepath.Join(t.TempDir(), "pool")
	var err error
	out := captureWarnLog(t, func(ctx context.Context) {
		_, err = writePool(ctx, pool, []Result{merged}, nil, "")
	})
	require.NoError(t, err)
	assert.Contains(t, out, "bruce")
	assert.Contains(t, out, "chunk 1 refused", "the warning must name the bin, not the whole persona")
	assert.NotContains(t, out, "contributed nothing",
		"bruce landed a real finding from its clean bin")
}

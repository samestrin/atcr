package fanout

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An agent whose response was truncated on finish_reason=length with zero surviving
// findings contributed NOTHING to the merged pool while the run still reports success.
// PoolSummary.TruncatedZeroFindings has counted exactly this since Epic 19.5, but the
// count only ever reached summary.json — nothing printed it, so the sole console
// surface was a per-agent WARN line an operator had to go looking for. Observed
// 2026-08-14: glm-5.2, minimax-m2.7, kimi-k3 and minimax-m3 all truncated across a
// whole baseline scan and the run summary said nothing.
//
// The tally already existed; only the emission was missing.
func TestWritePool_WarnsWhenAnAgentTruncatedWithZeroFindings(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""},
		{Agent: "kai", Status: StatusOK, ResponseTruncated: true, Content: ""},
		{Agent: "bruce", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e"},
	}
	pool := filepath.Join(t.TempDir(), "pool")

	var sum Summary
	var err error
	out := captureStderr(t, func() {
		sum, err = WritePool(pool, results, nil)
	})
	require.NoError(t, err)
	require.Equal(t, 3, sum.Total)

	assert.Contains(t, out, "truncated", "the run-level runaway must reach the console, not just summary.json")
	assert.Contains(t, out, "greta", "the warning must name WHICH agents contributed nothing")
	assert.Contains(t, out, "kai")
	assert.NotContains(t, out, "bruce", "an agent that landed findings is not a runaway")
}

// The warning's remedy ("raise their output cap") is only half the story: the output
// cap is subtracted from the SAME context window the diff is packed into, so raising
// it narrows that agent's input budget — and a cap within promptOverheadTokens (4096)
// of the resolved window collapses it to zero (payload.EffectiveByteBudget returns 0),
// which the bulk path degrades to a one-file review that still exits 0. An operator
// reading only this line follows it straight into a false-clean run, because the
// tradeoff is documented only in the flag help and docs/registry.md — neither of which
// is on screen when this fires.
func TestWritePool_TruncationWarning_NamesTheInputBudgetTradeoff(t *testing.T) {
	results := []Result{
		{Agent: "greta", Status: StatusOK, ResponseTruncated: true, Content: ""},
	}
	pool := filepath.Join(t.TempDir(), "pool")

	out := captureStderr(t, func() {
		_, err := WritePool(pool, results, nil)
		require.NoError(t, err)
	})

	assert.Contains(t, out, "input budget",
		"the remedy must name what raising the cap costs, not only what it buys")
	assert.Contains(t, out, "context window",
		"the tradeoff is only intelligible as a split of one window")
	assert.Contains(t, out, "4096",
		"the operator needs the boundary at which the input budget reaches zero")
}

// A clean run stays silent — a warning printed on every review is one nobody reads,
// the rule the vocabulary diagnostics already follow.
func TestWritePool_SilentWhenNoAgentTruncatedToZero(t *testing.T) {
	results := []Result{{Agent: "bruce", Status: StatusOK, Content: "HIGH|x.go:1|p|f|correctness|5|e"}}
	pool := filepath.Join(t.TempDir(), "pool")
	out := captureStderr(t, func() {
		_, err := WritePool(pool, results, nil)
		require.NoError(t, err)
	})
	assert.NotContains(t, out, "truncated")
}

package scorecard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/fanout"
	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitForReconcile_BridgesPoolSummaryAndFindings verifies the shared bridge
// both reconcile entry points (CLI + MCP) call: it sources per-reviewer usage
// from the fan-out pool summary.json and finding counts from the reconcile
// result, producing the same records regardless of caller (TD-005).
func TestEmitForReconcile_BridgesPoolSummaryAndFindings(t *testing.T) {
	reviewDir := t.TempDir()
	// HOME override routes the default scorecard store into a temp config dir
	// (darwin UserConfigDir is HOME-derived), so the test never touches real config.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	// Persist a pool summary carrying per-agent model + usage + latency.
	pool := filepath.Join(reviewDir, "sources", "pool")
	_, err := fanout.WritePool(pool, []fanout.Result{
		{Agent: "bruce", Status: fanout.StatusOK, Content: "x", Model: "claude-sonnet-4-6", TokensIn: 14200, TokensOut: 4000, DurationMS: 9100},
		{Agent: "greta", Status: fanout.StatusOK, Content: "x", Model: "claude-haiku-4-5", TokensIn: 8000, TokensOut: 2000, DurationMS: 5000},
	})
	require.NoError(t, err)

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce", "greta"}}},
			{Finding: reconcile.Finding{File: "b.go", Line: 2, Problem: "p2", Reviewers: []string{"bruce"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 3, "2 reviewer records + 1 aggregate")

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, "claude-sonnet-4-6", bruce.Model, "model sourced from pool summary")
	assert.Equal(t, 14200, bruce.TokensIn)
	assert.Equal(t, 2, bruce.FindingsRaised)
	assert.Equal(t, 1, bruce.FindingsCorroborated)
	assert.EqualValues(t, 9100, bruce.LatencyMS)
	assert.InDelta(t, 0.1026, bruce.CostUSD, 1e-9, "cost derived at emit time from model+tokens")
}

// TestEmitForReconcile_NoPoolSummaryDegrades verifies a path-anchored review with
// no fan-out pool summary still emits records, with reviewers recovered from the
// findings (no usage metadata).
func TestEmitForReconcile_NoPoolSummaryDegrades(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{}) // must not panic despite missing pool summary

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)
	require.Len(t, recs, 2, "1 reviewer + 1 aggregate even without pool summary")
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, 1, bruce.FindingsRaised)
	assert.Empty(t, bruce.Model, "no usage metadata without pool summary")
}

// TestEmitForReconcile_NoScorecardSuppresses verifies the --no-scorecard flag,
// threaded through the shared bridge as EmitOpts.NoScorecard, prevents any
// record — and the store directory itself — from being written.
func TestEmitForReconcile_NoScorecardSuppresses(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{NoScorecard: true})

	dir, err := DefaultDir()
	require.NoError(t, err)
	_, statErr := os.Stat(dir)
	require.True(t, os.IsNotExist(statErr), "suppressed run must not create the store directory")
}

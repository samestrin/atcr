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
	}, nil)

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

// TestEmitForReconcile_RoutedFindingsStayInDenominator pins the trust-inflation
// fix for Epic 35.16.6.5: Tier 4 routing removes a finding from res.Findings
// BEFORE this bridge reads it, so a reviewer's uncorroborated hallucinated-path
// singletons would silently leave the FindingsRaised denominator — the exact
// evidence that should DEPRESS its corroboration rate. res.Unresolved must be
// counted as raised-but-never-corroborated, so routing a phantom lowers the
// rate instead of raising it.
func TestEmitForReconcile_RoutedFindingsStayInDenominator(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce", "greta"}}},
		},
		// Two singletons bruce raised against files that do not exist, routed to
		// the sidecar by the Tier 4 content check.
		Unresolved: []reconcile.JSONFinding{
			{File: "phantom1.go", Line: 3, Problem: "ghost1", Reviewers: []string{"bruce"}},
			{File: "phantom2.go", Line: 9, Problem: "ghost2", Reviewers: []string{"bruce"}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, 3, bruce.FindingsRaised,
		"the two routed phantoms are still findings bruce raised")
	assert.Equal(t, 1, bruce.FindingsCorroborated,
		"a routed finding is never corroborated — it is the fabrication evidence itself")
	assert.Equal(t, 2, bruce.FindingsSolo)
	assert.InDelta(t, 1.0/3.0, bruce.CorroborationRate, 1e-9,
		"routing must DEPRESS the rate; dropping the phantoms would report 1.00")

	greta := findReviewer(recs, "greta")
	require.NotNil(t, greta)
	assert.Equal(t, 1, greta.FindingsRaised, "greta raised no routed finding")
	assert.Equal(t, 1, greta.FindingsCorroborated)
}

// TestEmitForReconcile_RoutedOnlyReviewerStillRecorded pins the companion hole:
// a reviewer whose every finding was routed to the sidecar has no entry in
// res.Findings at all, so without registering the routed records' reviewers it
// would vanish from the scorecard entirely — no record, no rate, and therefore
// no trust penalty for a run that produced nothing but phantoms.
func TestEmitForReconcile_RoutedOnlyReviewerStillRecorded(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"greta"}}},
		},
		Unresolved: []reconcile.JSONFinding{
			{File: "phantom.go", Line: 3, Problem: "ghost", Reviewers: []string{"bruce"}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce, "a reviewer whose only finding was routed must still get a record")
	assert.Equal(t, 1, bruce.FindingsRaised)
	assert.Zero(t, bruce.FindingsCorroborated)
	assert.Zero(t, bruce.CorroborationRate)
}

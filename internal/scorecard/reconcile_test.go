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

// TestEmitForReconcile_DocShieldRoutingIsNotCharged pins the one consequence of
// a wrong routing that the preserved sidecar cannot undo.
//
// A finding routed because its subject was named only in a documentation file is
// routed on a HEURISTIC — isDocExt classifies by extension, and the extension is
// not a reliable proxy for "cannot declare" (that is the whole of AC2). Every
// other consumer of a routed record is recoverable: unresolved.json preserves it,
// and a human or a later run can read it back. The scorecard is not — the routed
// finding is added to the reviewer's denominator, never corroborated, and nothing
// reads unresolved.json back into it, so a heuristic misfire durably depresses
// CorroborationRate and moves trustExempt/demoteByTrust on unrelated runs through
// the 180-day window.
//
// So a doc-shield routing is preserved everywhere it is recoverable and charged
// nowhere it is not. A true no-match — the anchor appears nowhere in the tree at
// all — is still charged in full; that is the evidence the denominator exists for.
func TestEmitForReconcile_DocShieldRoutingIsNotCharged(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"bruce", "greta"}}},
		},
		Unresolved: []reconcile.JSONFinding{
			// Named nowhere in the tree: a true phantom, charged.
			{File: "phantom.go", Line: 3, Problem: "ghost", Reviewers: []string{"bruce"}},
			// Named only in a documentation file: routed on the heuristic, so it
			// stays in the sidecar but must not reach the denominator.
			{File: "guide.go", Line: 9, Problem: "Callout is unsafe", Reviewers: []string{"bruce"},
				UnresolvedReason: reconcile.UnresolvedReasonDocShield},
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
	assert.Equal(t, 2, bruce.FindingsRaised,
		"the true phantom is charged; the doc-shield routing is not")
	assert.Equal(t, 1, bruce.FindingsCorroborated)
	assert.InDelta(t, 0.5, bruce.CorroborationRate, 1e-9,
		"charging the doc-shield routing too would report 1/3 on a heuristic")
	assert.Equal(t, 1, bruce.FindingsDocShielded,
		"the carve-out must be visible on the record: a rate computed with an exemption is not the same number as one computed without")

	greta := findReviewer(recs, "greta")
	require.NotNil(t, greta)
	assert.Equal(t, 0, greta.FindingsDocShielded, "greta was granted no exemption")

	// The aggregate record must sum the carve-out counter too: docs/scorecard.md
	// promises findings_* sum across reviewers, and a board reading the aggregate
	// alongside a reviewer row would otherwise see an exemption the total hides.
	agg := recs[len(recs)-1]
	require.Equal(t, RecordTypeAggregate, agg.RecordType)
	assert.Equal(t, 1, agg.FindingsDocShielded,
		"the aggregate sums the reviewer rows' shielded counts (scorecard.go agg.FindingsDocShielded += ...)")
}

// TestEmitForReconcile_DocShieldOnlyReviewerStillRecorded pins that excluding a
// doc-shield routing from the COUNT does not also erase the reviewer. A reviewer
// whose every finding was doc-shield-routed still gets a record — otherwise the
// exclusion would hand back the disappearing-reviewer hole that
// TestEmitForReconcile_RoutedOnlyReviewerStillRecorded closes.
func TestEmitForReconcile_DocShieldOnlyReviewerStillRecorded(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"greta"}}},
		},
		Unresolved: []reconcile.JSONFinding{
			{File: "guide.go", Line: 9, Problem: "Callout is unsafe", Reviewers: []string{"bruce"},
				UnresolvedReason: reconcile.UnresolvedReasonDocShield},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce, "a doc-shield-routed reviewer must still get a record")
	assert.Equal(t, 0, bruce.FindingsRaised,
		"nothing chargeable was raised, but the reviewer is on the board")
	assert.Equal(t, 1, bruce.FindingsDocShielded,
		"a reviewer whose whole run was exempted must not look like a reviewer who raised nothing")
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

// TestEmitForReconcile_RoutedEmptyReviewerNameNotRegistered pins the empty-name
// guard on the routed-findings loop. `Reviewers` is free text carried from a
// reviewer's own findings.txt, so an empty cell reaches here; registering it
// would emit a scorecard record under the empty name — a phantom reviewer with
// its own corroboration rate that `atcr scorecard` would then list forever.
func TestEmitForReconcile_RoutedEmptyReviewerNameNotRegistered(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		// An empty cell on BOTH loops: the surviving-findings loop and the routed
		// loop each carry their own copy of the guard, and a test that exercises
		// only one leaves its twin free to regress.
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Reviewers: []string{"", "greta"}}},
		},
		Unresolved: []reconcile.JSONFinding{
			{File: "phantom.go", Line: 3, Problem: "ghost", Reviewers: []string{"", "bruce"}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}

	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	assert.Nil(t, findReviewer(recs, ""),
		"an empty reviewer name must never earn a record of its own")
	require.NotNil(t, findReviewer(recs, "bruce"),
		"the real reviewer on the same routed finding must still be recorded")
	require.NotNil(t, findReviewer(recs, "greta"),
		"the real reviewer on the same surviving finding must still be recorded")
}

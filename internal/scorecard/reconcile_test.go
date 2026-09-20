package scorecard

import (
	"os"
	"path/filepath"
	"strings"
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
	assert.Equal(t, 0.0, bruce.CorroborationRate,
		"0.00 alongside a nonzero shield count is a zero denominator, not a corroboration failure (docs/scorecard.md)")
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

// --- Phase 3 (Story 03): Category threading ------------------------------
//
// These tests drive the REAL EmitForReconcile path and read the written JSONL
// back, per AC 03-02's requirement that the category is proven to survive to
// disk rather than asserted on a hand-built Record.

// TestEmitForReconcile_CategoryThreadedFromPrimaryStream is AC 03-02 Happy Path
// Scenario 1 at the primary (res.Findings) construction site.
func TestEmitForReconcile_CategoryThreadedFromPrimaryStream(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "api-contract", Reviewers: []string{"vera"}}},
			{Finding: reconcile.Finding{File: "b.go", Line: 2, Problem: "p2", Category: "error-handling", Reviewers: []string{"dax"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	vera := findReviewer(recs, "vera")
	require.NotNil(t, vera)
	assert.Equal(t, []string{"api-contract"}, vera.CategoriesRaised)

	dax := findReviewer(recs, "dax")
	require.NotNil(t, dax)
	assert.Equal(t, []string{"error-handling"}, dax.CategoriesRaised)
}

// TestEmitForReconcile_CategoryThreadedFromUnresolvedStream is AC 03-02 Edge Case
// 1: the Tier-4-routed res.Unresolved construction site is the one a reader
// skims past, and a reviewer whose every finding was routed is reachable ONLY
// through it.
func TestEmitForReconcile_CategoryThreadedFromUnresolvedStream(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Unresolved: []reconcile.JSONFinding{
			{File: "ghost.go", Line: 7, Problem: "phantom", Category: "security", Reviewers: []string{"sasha"}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, 1, sasha.FindingsRaised, "a routed finding still charges the denominator")
	assert.Equal(t, []string{"security"}, sasha.CategoriesRaised,
		"the routed construction site must thread Category too")
}

// TestEmitForReconcile_ConsensusFilteredSingletonStillRecordsItsCategory closes
// the starvation loop the Phase 3 gate found.
//
// Under strict consensus an uncorroborated singleton is routed into
// res.Ambiguous unless trustExempt spares it — and trustExempt is OFF for a lens
// with no prior. Read only res.Findings and that lens's category never lands, so
// its own run reads out-of-remit, the opportunity filter deletes the record, the
// run count stays under the floor, and the prior that would have spared the
// finding is never earned. The lens is starved by its own missing prior.
//
// sasha below is exactly that lens: its only finding was filtered. The category
// must land; the COUNTS must not move.
func TestEmitForReconcile_ConsensusFilteredSingletonStillRecordsItsCategory(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			// Keeps sasha in in.Reviewers with a real, surviving finding, so the
			// test measures the CATEGORY stream rather than record creation.
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"sasha"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{Findings: []reconcile.Finding{
				{File: "b.go", Line: 9, Problem: "solo security nit", Category: "security", Reviewers: []string{"sasha"}},
			}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, []string{"correctness", "security"}, sasha.CategoriesRaised,
		"the filtered singleton's category is still evidence sasha's remit was in play")
	assert.Equal(t, 1, sasha.FindingsRaised,
		"a filtered finding must earn NO scoring credit — only the surviving one counts")
	assert.Equal(t, 0, sasha.FindingsCorroborated,
		"and it must never be read as corroboration")
}

// TestEmitForReconcile_AmbiguousSingularReviewerShapeIsTheProductionOne is the
// test the one above cannot be.
//
// singletonAmbiguousCluster normalises a one-reviewer finding BACK to the raw
// per-source shape (Reviewer set, Reviewers cleared), and a consensus-filtered
// finding is always one-reviewer — both consensus floors are HIGH/MEDIUM and
// ConfidenceFor only reaches those with 2+ distinct reviewers. So the SINGULAR
// branch is the one the motivating route actually takes. A fixture built with
// Reviewers populated exercises a shape that stream never produces: delete the
// singular fallback and the headline starvation fix stops working while the
// plural-shaped test stays green.
func TestEmitForReconcile_AmbiguousSingularReviewerShapeIsTheProductionOne(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			// Registers sasha, so the assertion is about the CATEGORY landing on
			// an existing record rather than about record creation.
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"sasha"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			{Findings: []reconcile.Finding{
				// The production shape: Reviewer singular, Reviewers nil.
				{File: "b.go", Line: 9, Problem: "solo security nit", Category: "security", Reviewer: "sasha"},
			}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, []string{"correctness", "security"}, sasha.CategoriesRaised,
		"the singular-Reviewer shape is what the consensus filter emits; reading only Reviewers attributes it to nobody")
	assert.Equal(t, 1, sasha.FindingsRaised, "still no scoring credit for a filtered finding")
}

// TestEmitForReconcile_AmbiguousNeverMintsAReviewerRecord is the boundary on the
// change above. The ambiguous stream widens what an EXISTING record knows about
// its case; it must not create one, or a change whose remit is categories would
// quietly add records carrying a zero denominator.
func TestEmitForReconcile_AmbiguousNeverMintsAReviewerRecord(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"bruce"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{
			// Per-source shape: Reviewer singular, and a lens present nowhere else.
			{Findings: []reconcile.Finding{
				{File: "b.go", Line: 9, Problem: "solo", Category: "security", Reviewer: "sasha"},
			}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	assert.Nil(t, findReviewer(recs, "sasha"),
		"a lens reachable only through a filtered cluster gets no record of its own")
	require.NotNil(t, findReviewer(recs, "bruce"))
}

// TestEmitForReconcile_CategoriesRaisedIsDedupedAndSorted is AC 03-02 Happy Path
// Scenario 2. Sorted output is asserted deliberately: the field is persisted for
// 180 days and compared across records, so a map-iteration-ordered slice would
// make two identical runs serialize differently.
func TestEmitForReconcile_CategoriesRaisedIsDedupedAndSorted(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "testing", Reviewers: []string{"dax"}}},
			{Finding: reconcile.Finding{File: "b.go", Line: 2, Problem: "p2", Category: "testing", Reviewers: []string{"dax"}}},
			{Finding: reconcile.Finding{File: "c.go", Line: 3, Problem: "p3", Category: "error-handling", Reviewers: []string{"dax"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	recs, err := ReadRecords(filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl"), ReadOpts{})
	require.NoError(t, err)

	dax := findReviewer(recs, "dax")
	require.NotNil(t, dax)
	assert.Equal(t, []string{"error-handling", "testing"}, dax.CategoriesRaised,
		"deduped once and in deterministic (sorted) order")
}

// TestEmitForReconcile_DocShieldedCategoryIsNotRaised is AC 03-02 Edge Case 2.
// The doc-shield carve-out already excludes these findings from FindingsRaised;
// the category set must follow the same rule or the shield leaks back in through
// the opportunity gate.
func TestEmitForReconcile_DocShieldedCategoryIsNotRaised(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "real", Category: "correctness", Reviewers: []string{"bruce"}}},
		},
		Unresolved: []reconcile.JSONFinding{
			{
				File: "README.md", Line: 3, Problem: "shielded", Category: "docs",
				Reviewers: []string{"bruce"}, UnresolvedReason: reconcile.UnresolvedReasonDocShield,
			},
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
	assert.Equal(t, []string{"correctness"}, bruce.CategoriesRaised)
	assert.NotContains(t, bruce.CategoriesRaised, "docs",
		"a doc-shielded finding is excluded from the category set exactly as it is from FindingsRaised")
}

// TestEmitForReconcile_EmptyAndOutOfVocabularyCategoriesAreDropped covers AC
// 03-01 Edge Case 2 and sprint-plan task 3.2 step 1a. A category that is not a
// reclib.Categories() member fails NEUTRAL: dropped from the persisted set,
// never trusted into the opportunity gate, and never a hard error that fails the
// caller's reconcile.
func TestEmitForReconcile_EmptyAndOutOfVocabularyCategoriesAreDropped(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "", Reviewers: []string{"bruce"}}},
			{Finding: reconcile.Finding{File: "b.go", Line: 2, Problem: "p2", Category: "NOT-A-CATEGORY", Reviewers: []string{"bruce"}}},
			{Finding: reconcile.Finding{File: "c.go", Line: 3, Problem: "p3", Category: "Correctness", Reviewers: []string{"bruce"}}},
			{Finding: reconcile.Finding{File: "d.go", Line: 4, Problem: "p4", Category: "state", Reviewers: []string{"bruce"}}},
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
	assert.Equal(t, []string{"state"}, bruce.CategoriesRaised,
		"empty, out-of-vocabulary and mis-cased values are all dropped; the findings still count")
	assert.Equal(t, 4, bruce.FindingsRaised,
		"dropping a category must NOT drop the finding from the denominator")
}

// TestEmitForReconcile_ReviewerWithNoFindingsHasNoCategories is AC 03-02 Edge
// Case 3: a reviewer present only in the pool summary must not inherit another
// reviewer's categories, and the key must be omitted rather than written empty.
func TestEmitForReconcile_ReviewerWithNoFindingsHasNoCategories(t *testing.T) {
	reviewDir := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	pool := filepath.Join(reviewDir, "sources", "pool")
	_, err := fanout.WritePool(pool, []fanout.Result{
		{Agent: "bruce", Status: fanout.StatusOK, Content: "x", Model: "m"},
		{Agent: "otto", Status: fanout.StatusOK, Content: "x", Model: "m"},
	}, nil)
	require.NoError(t, err)

	res := reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p1", Category: "correctness", Reviewers: []string{"bruce"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	}
	EmitForReconcile(reviewDir, res, EmitOpts{})

	cfg, err := os.UserConfigDir()
	require.NoError(t, err)
	path := filepath.Join(cfg, "atcr", "scorecard", "2026-06.jsonl")
	recs, err := ReadRecords(path, ReadOpts{})
	require.NoError(t, err)

	otto := findReviewer(recs, "otto")
	require.NotNil(t, otto)
	assert.Empty(t, otto.CategoriesRaised, "a silent reviewer inherits nobody's categories")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.Contains(line, `"reviewer":"otto"`) {
			assert.NotContains(t, line, "categories_raised",
				"omitempty must omit the key entirely for an empty set")
		}
	}
}

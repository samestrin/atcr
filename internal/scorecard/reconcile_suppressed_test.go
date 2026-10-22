package scorecard

import (
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitForReconcile_SuppressedOnlyReviewerIsRaisedNeverCorroborated is Epic
// 40.0 T5 / AC5: a finding an .atcr/ignore.yaml rule suppressed is removed from
// res.Findings before this bridge runs, yet it was raised. It counts in
// FindingsRaised, never in FindingsCorroborated, and a reviewer whose every
// finding was suppressed still gets a record.
func TestEmitForReconcile_SuppressedOnlyReviewerIsRaisedNeverCorroborated(t *testing.T) {
	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Suppressed: []reconcile.JSONFinding{
			{File: "gen/x.go", Line: 4, Problem: "generated noise", Reviewers: []string{"r1"}, Category: "style"},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})

	r1 := findReviewer(recs, "r1")
	require.NotNil(t, r1, "a reviewer whose only finding was suppressed must still get a record")
	assert.Equal(t, 1, r1.FindingsRaised)
	assert.Zero(t, r1.FindingsCorroborated)
	assert.Zero(t, r1.WeightedCredit, "a suppressed finding earns no credit")
	assert.Equal(t, []string{"style"}, r1.CategoriesRaised)
}

// TestEmitForReconcile_SuppressedMatchesThePrimaryStreamCounts pins the other
// half of the success line: the same finding in res.Findings is unchanged by
// this task — still one raised, none corroborated — so suppressing it moves no
// count a trust prior reads.
func TestEmitForReconcile_SuppressedMatchesThePrimaryStreamCounts(t *testing.T) {
	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "gen/x.go", Line: 4, Problem: "generated noise", Reviewers: []string{"r1"}, Category: "style"}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})

	r1 := findReviewer(recs, "r1")
	require.NotNil(t, r1)
	assert.Equal(t, 1, r1.FindingsRaised)
	assert.Zero(t, r1.FindingsCorroborated)
	assert.Zero(t, r1.FindingsRouted)
}

// TestEmitForReconcile_SuppressedAgreementIsNotCorroboration: two reviewers on
// one suppressed finding are each charged one raised finding and neither is
// corroborated. The rule that hid the finding says nothing about whether the
// agreement was right, so it buys no corroboration.
func TestEmitForReconcile_SuppressedAgreementIsNotCorroboration(t *testing.T) {
	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Suppressed: []reconcile.JSONFinding{
			{File: "gen/x.go", Line: 4, Problem: "generated noise", Reviewers: []string{"r1", " R2 ", ""}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})

	for _, name := range []string{"r1", "r2"} {
		rec := findReviewer(recs, name)
		require.NotNil(t, rec, name)
		assert.Equal(t, 1, rec.FindingsRaised, name)
		assert.Zero(t, rec.FindingsCorroborated, name)
	}
	assert.Nil(t, findReviewer(recs, ""), "a blank reviewer cell must not mint a record")
}

// TestEmitForReconcile_SuppressedDocShieldReasonIsStillCharged: the doc-shield
// carve-out belongs to the Tier 4 routing decision, not to suppression. A
// suppressed record never carries that reason through, so it is always charged.
func TestEmitForReconcile_SuppressedDocShieldReasonIsStillCharged(t *testing.T) {
	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Suppressed: []reconcile.JSONFinding{
			{File: "docs/a.md", Line: 1, Problem: "p", Reviewers: []string{"r1"},
				UnresolvedReason: reconcile.UnresolvedReasonDocShield},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})

	r1 := findReviewer(recs, "r1")
	require.NotNil(t, r1)
	assert.Equal(t, 1, r1.FindingsRaised)
	assert.Zero(t, r1.FindingsDocShielded)
}

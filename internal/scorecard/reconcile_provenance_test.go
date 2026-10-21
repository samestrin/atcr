package scorecard

import (
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Epic 35.16.11.3.1 T1: CategoriesRaised reads each lens's OWN category off the
// merged record's SourceCategories, not Merge's cluster-modal Category.

// mixedCluster is one lens saying security and two saying performance: the
// cluster-modal category is performance, so bruce's own word loses the vote.
func mixedCluster() reconcile.Merged {
	return reclib.Merge([]reclib.Finding{
		{File: "a.go", Line: 1, Problem: "p", Category: "security", Reviewer: "Bruce"},
		{File: "a.go", Line: 1, Problem: "p", Category: "performance", Reviewer: "greta"},
		{File: "a.go", Line: 1, Problem: "p", Category: "performance", Reviewer: "kai"},
	})
}

func TestEmitForReconcile_MixedCategoryClusterYieldsEachLensOwnWord(t *testing.T) {
	m := mixedCluster()
	require.Equal(t, "performance", m.Category, "precondition: the modal value is not bruce's word")

	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{m},
		Summary:  reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, []string{"security"}, bruce.CategoriesRaised,
		"bruce's own word, not the cluster-modal performance")
	for _, name := range []string{"greta", "kai"} {
		r := findReviewer(recs, name)
		require.NotNil(t, r, name)
		assert.Equal(t, []string{"performance"}, r.CategoriesRaised, name)
	}
	assert.Equal(t, 1, bruce.FindingsRaised, "provenance moves no count")
	assert.Equal(t, 1, bruce.FindingsCorroborated, "provenance moves no count")
}

func TestEmitForReconcile_UnresolvedStreamYieldsEachLensOwnWord(t *testing.T) {
	// Route the mixed cluster the way RunReconcile does: res.Unresolved holds the
	// JSONFindings record derived from the merged finding.
	routed := reconcile.Result{Findings: []reconcile.Merged{mixedCluster()}}.JSONFindings()
	require.Len(t, routed, 1)

	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Unresolved: routed,
		Summary:    reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})

	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, []string{"security"}, bruce.CategoriesRaised,
		"the routed copy must carry bruce's own word too")
	greta := findReviewer(recs, "greta")
	require.NotNil(t, greta)
	assert.Equal(t, []string{"performance"}, greta.CategoriesRaised)
}

func TestEmitForReconcile_ReviewerWithTwoWordsInOneClusterRaisesBoth(t *testing.T) {
	m := reclib.Merge([]reclib.Finding{
		{File: "a.go", Line: 1, Problem: "p", Category: "security", Reviewer: "bruce"},
		{File: "a.go", Line: 1, Problem: "p", Category: "secret", Reviewer: "bruce"},
		{File: "a.go", Line: 1, Problem: "p", Category: "secret", Reviewer: "greta"},
	})
	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{m},
		Summary:  reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})
	bruce := findReviewer(recs, "bruce")
	require.NotNil(t, bruce)
	assert.Equal(t, []string{"secret", "security"}, bruce.CategoriesRaised)
}

func TestEmitForReconcile_NoSourceCategoriesFallsBackToCategory(t *testing.T) {
	// A Merged built without provenance (a hand-built Result, or a producer older
	// than SourceCategories) keeps the cluster value rather than raising nothing.
	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			{Finding: reconcile.Finding{File: "a.go", Line: 1, Problem: "p", Category: "state", Reviewers: []string{"bruce", "greta"}}},
		},
		Summary: reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})
	for _, name := range []string{"bruce", "greta"} {
		r := findReviewer(recs, name)
		require.NotNil(t, r, name)
		assert.Equal(t, []string{"state"}, r.CategoriesRaised, name)
	}
}

func TestEmitForReconcile_GrayPairTieKeepsEachLensOwnWord(t *testing.T) {
	// Normalising the ambiguous stream to its cluster-modal value is REJECTED.
	// On this gray-zone pair the modal value is a 1-1 tie broken alphabetically
	// (performance), which would stamp sasha with a word sasha never said.
	pair := []reclib.Finding{
		{File: "a.go", Line: 1, Problem: "p1", Category: "security", Reviewer: "sasha"},
		{File: "a.go", Line: 2, Problem: "p2", Category: "performance", Reviewer: "dax"},
	}
	require.Equal(t, "performance", reclib.ModalCategory(pair), "precondition: the tie mislabels sasha")

	recs := emitAndRead(t, t.TempDir(), reconcile.Result{
		Findings: []reconcile.Merged{
			// Records for both lenses; the ambiguous stream mints none.
			{Finding: reconcile.Finding{File: "z.go", Line: 1, Problem: "z", Category: "testing", Reviewers: []string{"sasha", "dax"}}},
		},
		Ambiguous: []reconcile.AmbiguousCluster{{Findings: pair}},
		Summary:   reconcile.Summary{ReconciledAt: "2026-06-14T10:00:00Z"},
	})
	sasha := findReviewer(recs, "sasha")
	require.NotNil(t, sasha)
	assert.Equal(t, []string{"security", "testing"}, sasha.CategoriesRaised)
	dax := findReviewer(recs, "dax")
	require.NotNil(t, dax)
	assert.Equal(t, []string{"performance", "testing"}, dax.CategoriesRaised)
}

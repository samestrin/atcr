package reconcile

import (
	"testing"
	"time"
)

func recAt() Options { return Options{ReconciledAt: time.Unix(1700000000, 0).UTC()} }

func TestReconcile_TwoReviewersAgreeHighConfidence(t *testing.T) {
	sources := []Source{
		{Name: "pool", Findings: []Finding{
			mf("HIGH", "auth.go", 42, "token never expires here", "fix", "security", 15, "e", "greta"),
		}},
		{Name: "host", Findings: []Finding{
			mf("HIGH", "auth.go", 43, "token never expires here", "fix", "security", 15, "e", "host"),
		}},
	}
	res := Reconcile(sources, recAt())
	length(t, res.Findings, 1, "co-located identical findings collapse")
	eq(t, res.Findings[0].Confidence, ConfHigh, "two reviewers → HIGH")
	deepEq(t, res.Findings[0].Reviewers, []string{"greta", "host"}, "reviewers unioned")
	eq(t, res.Summary.ClustersCollapsed, 1, "one cluster collapsed")
	deepEq(t, res.Summary.PerSourceCounts, map[string]int{"pool": 1, "host": 1}, "per-source counts")
}

func TestReconcile_SortedBySeverityThenLocation(t *testing.T) {
	// All findings share one reviewer so the panel stays below the consensus-filter
	// floor (consensusMinReviewers): this test isolates sort order, so the epic-14.2
	// singleton filter must not fire and drop the LOW/MEDIUM findings under test.
	sources := []Source{{Name: "pool", Findings: []Finding{
		mf("LOW", "z.go", 5, "p1", "f", "style", 1, "e", "a"),
		mf("CRITICAL", "a.go", 1, "p2", "f", "sec", 9, "e", "a"),
		mf("MEDIUM", "m.go", 3, "p3", "f", "test", 4, "e", "a"),
	}}}
	res := Reconcile(sources, recAt())
	length(t, res.Findings, 3, "three findings")
	eq(t, res.Findings[0].Severity, "CRITICAL", "critical first")
	eq(t, res.Findings[1].Severity, "MEDIUM", "medium second")
	eq(t, res.Findings[2].Severity, "LOW", "low last")
}

func TestReconcile_AmbiguousAlwaysNonNilEvenWhenEmpty(t *testing.T) {
	res := Reconcile(nil, recAt())
	isTrue(t, res.Ambiguous != nil, "ambiguous slice is non-nil")
	length(t, res.Ambiguous, 0, "empty")
	eq(t, res.Summary.TotalFindings, 0, "no findings")
}

func TestReconcile_OutOfScopeCountedNotDropped(t *testing.T) {
	sources := []Source{{Name: "pool", Findings: []Finding{
		mf("HIGH", "legacy.go", 7, "preexisting", "n/a", CategoryOutOfScope, 0, "e", "greta"),
		mf("HIGH", "auth.go", 1, "real bug", "fix", "security", 10, "e", "greta"),
	}}}
	res := Reconcile(sources, recAt())
	length(t, res.Findings, 2, "both kept")
	eq(t, res.Summary.OutOfScope, 1, "out-of-scope finding is counted, not dropped")
}

// TestReconcile_SkippedSourcesEmptyFromLibrary proves the library leaves the
// skipped-source bookkeeping empty — that field is stamped by an embedding I/O
// layer after Reconcile returns (Epic 8.0 Phase 2 Clarification Q3).
func TestReconcile_SkippedSourcesEmptyFromLibrary(t *testing.T) {
	res := Reconcile([]Source{{Name: "pool"}}, recAt())
	length(t, res.Summary.SkippedSources, 0, "library produces no skipped sources")
	eq(t, res.Summary.SkippedSourceCount, 0, "count zero")
	isTrue(t, res.Summary.SkippedSources != nil, "always serializes [] not null")
}

// TestSortMerged_StrictTotalOrderOnColocatedDistinctFindings asserts that two
// distinct findings sharing the same severity+file+line produce identical
// sort output regardless of input order.  Without a Problem tiebreak, SliceStable
// depends on input order for equal elements — a future dedupeCluster refactor
// could silently reorder output.
func TestSortMerged_StrictTotalOrderOnColocatedDistinctFindings(t *testing.T) {
	a := Merged{Finding: Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "alpha problem"}}
	b := Merged{Finding: Finding{Severity: "HIGH", File: "a.go", Line: 1, Problem: "beta problem"}}

	order1 := []Merged{a, b}
	sortMerged(order1)

	order2 := []Merged{b, a}
	sortMerged(order2)

	if order1[0].Problem != order2[0].Problem || order1[1].Problem != order2[1].Problem {
		t.Errorf("sortMerged is not a strict total order: same findings in different input order yield different output\n  order1[0]=%q, order2[0]=%q",
			order1[0].Problem, order2[0].Problem)
	}
}

func TestSortMerged_NormalizesMixedCaseSeverity(t *testing.T) {
	m := []Merged{
		{Finding: Finding{Severity: "low", File: "a.go", Line: 1}},
		{Finding: Finding{Severity: "high", File: "b.go", Line: 1}},
	}
	sortMerged(m)
	eq(t, NormalizeSeverity(m[0].Severity), "HIGH", "high sorts first despite lowercase")
}

// TestReconcile_VerificationDroppedByMerge feeds a Finding with a populated
// Verification through the full Reconcile pipeline and asserts the output
// finding's Verification is nil (TD-005: Verification is stamped post-reconcile
// by the caller; the library must not propagate it through Merge).
func TestReconcile_VerificationDroppedByMerge(t *testing.T) {
	v := &Verification{Verdict: "CONFIRMED", Skeptic: "greta"}
	sources := []Source{{
		Name: "s1",
		Findings: []Finding{
			{File: "a.go", Line: 1, Problem: "p", Fix: "f", Category: "sec", Reviewer: "greta", Verification: v},
		},
	}}
	result := Reconcile(sources, Options{})
	if len(result.Findings) != 1 {
		t.Fatalf("expected 1 merged finding, got %d", len(result.Findings))
	}
	if result.Findings[0].Verification != nil {
		t.Errorf("Reconcile must not carry Verification through merge (TD-005), got %+v", result.Findings[0].Verification)
	}
}

func TestAllFindings_FlattensInSourceOrder(t *testing.T) {
	sources := []Source{
		{Name: "a", Findings: []Finding{mf("LOW", "a.go", 1, "p", "f", "s", 1, "e", "r")}},
		{Name: "b", Findings: []Finding{
			mf("HIGH", "b.go", 2, "p", "f", "s", 1, "e", "r"),
			mf("MEDIUM", "c.go", 3, "p", "f", "s", 1, "e", "r"),
		}},
	}
	all := AllFindings(sources)
	length(t, all, 3, "flattened")
	eq(t, all[0].File, "a.go", "source order preserved")
	eq(t, all[1].File, "b.go", "source order preserved")
	eq(t, all[2].File, "c.go", "source order preserved")
}

func TestReconcile_SummaryCountsAmbiguousAndNoise(t *testing.T) {
	// Dense pair (greta+kai merge) provides the corroboration context that lets a
	// third, unrelated finding be isolated as DBSCAN noise.
	sources := []Source{{Name: "pool", Findings: []Finding{
		mf("HIGH", "a.go", 1, "token never expires unchecked here", "", "security", 15, "e", "greta"),
		mf("HIGH", "a.go", 1, "token never expires unchecked here", "", "security", 15, "e", "kai"),
		mf("HIGH", "a.go", 1, "completely different spurious claim", "", "security", 15, "e", "mira"),
	}}}
	res := Reconcile(sources, recAt())
	eq(t, res.Summary.AmbiguousCount, 1, "one noise entry in ambiguous sidecar")
	eq(t, res.Summary.NoiseCount, 1, "the single entry is noise")
}

package reconcile

import "testing"

// mf builds a per-source finding with the merge-relevant fields.
func mf(sev, file string, line int, problem, fix, category string, est int, evidence, reviewer string) Finding {
	return Finding{
		Severity: sev, File: file, Line: line, Problem: problem, Fix: fix,
		Category: category, EstMinutes: est, Evidence: evidence, Reviewer: reviewer,
	}
}

func TestMerge_OutOfScopeFailClosed(t *testing.T) {
	// Fail-closed: out-of-scope suppresses gating only when EVERY reviewer
	// tagged the finding out-of-scope. A 2-vs-1 out-of-scope majority must not
	// drop the real category — the real category wins and the finding still gates.
	mixed := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "kai"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "mira"),
	}
	eq(t, Merge(mixed).Category, "security", "real category wins over an out-of-scope majority")

	unanimous := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", CategoryOutOfScope, 10, "e", "kai"),
	}
	eq(t, Merge(unanimous).Category, CategoryOutOfScope, "unanimous out-of-scope stays annotated")
}

func TestMerge_ReviewersJoinedDedupedSorted(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e1", "kai"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e2", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "e3", "kai"), // dup reviewer
	})
	deepEq(t, m.Reviewers, []string{"greta", "kai"}, "reviewers deduped and sorted")
	eq(t, m.Confidence, ConfHigh, "2 distinct reviewers → HIGH")
}

func TestMerge_SingleReviewerIsMedium(t *testing.T) {
	m := Merge([]Finding{mf("LOW", "a.go", 1, "p", "f", "style", 5, "e", "greta")})
	eq(t, m.Confidence, ConfMedium, "single reviewer → MEDIUM")
	eq(t, m.Disagreement, "", "no disagreement for a single finding")
}

func TestMerge_MaxSeverityWithDisagreement(t *testing.T) {
	m := Merge([]Finding{
		mf("CRITICAL", "a.go", 42, "long detailed problem text", "f", "sec", 30, "e", "greta"),
		mf("LOW", "a.go", 42, "short", "fix detail longer", "perf", 60, "e", "kai"),
	})
	eq(t, m.Severity, "CRITICAL", "max severity wins")
	eq(t, m.Disagreement, "LOW vs CRITICAL", "disagreement annotation")
}

func TestMerge_LongestProblemAndFix_MaxEst(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "short", "f1", "sec", 15, "e", "greta"),
		mf("HIGH", "a.go", 1, "a much longer and more detailed problem", "longer fix text", "sec", 45, "e", "kai"),
	})
	eq(t, m.Problem, "a much longer and more detailed problem", "longest problem")
	eq(t, m.Fix, "longer fix text", "longest fix")
	eq(t, m.EstMinutes, 45, "max estimate (most pessimistic)")
}

func TestMerge_ModalCategoryAlphabeticTiebreak(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "performance", 10, "e", "b"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "c"),
	})
	eq(t, m.Category, "security", "modal category")

	tie := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "correctness", 10, "e", "b"),
	})
	eq(t, tie.Category, "correctness", "alphabetical tiebreak")
}

func TestMerge_EmptyGroupDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Merge(nil) panicked: %v", r)
		}
	}()
	_ = Merge(nil)
}

func TestMerge_EmptyCategoryDoesNotHijackTie(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "", 10, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "security", 10, "e", "b"),
	})
	eq(t, m.Category, "security", "real category preferred over empty on a tie")
}

func TestMerge_EvidenceReviewerPrefixedWhenMultiple(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "saw X", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", 10, "saw Y", "kai"),
	})
	contains(t, m.Evidence, "[greta] saw X", "reviewer-prefixed evidence")
	contains(t, m.Evidence, "[kai] saw Y", "reviewer-prefixed evidence")
}

func TestMerge_ModalCategoryCanonicalizesCase(t *testing.T) {
	nonCanonical := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", "Out-Of-Scope", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "Out-Of-Scope", 10, "e", "kai"),
	}
	eq(t, Merge(nonCanonical).Category, CategoryOutOfScope,
		"non-canonical 'Out-Of-Scope' normalized to canonical 'out-of-scope'")

	mixedCase := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", "OUT-OF-SCOPE", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "out-of-scope", 10, "e", "kai"),
	}
	eq(t, Merge(mixedCase).Category, CategoryOutOfScope,
		"all-caps and canonical variants collapse to the same canonical form")
}

func TestMerge_ModalCategoryCanonicalizesAllCategories(t *testing.T) {
	m := Merge([]Finding{
		mf("HIGH", "a.go", 1, "p", "f", "Security", 10, "e", "greta"),
		mf("HIGH", "a.go", 1, "p", "f", "SECURITY", 10, "e", "kai"),
	})
	eq(t, m.Category, "security", "non-canonical category casing normalized to lowercase")
}

func TestMerge_MixedCaseDuplicateIsNotADisagreement(t *testing.T) {
	got := Merge([]Finding{
		{Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "CRITICAL", File: "a.go", Line: 1, Reviewer: "r2"},
	})
	eq(t, got.Disagreement, "", "mixed-case duplicate is one severity, no disagreement")
	eq(t, got.Severity, "CRITICAL", "canonical CRITICAL")
}

func TestMerge_NormalizesMixedCaseSeverity(t *testing.T) {
	got := Merge([]Finding{
		{Severity: "critical", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "LOW", File: "a.go", Line: 1, Reviewer: "r2"},
	})
	eq(t, SeverityRank[NormalizeSeverity(got.Severity)], 4, "lowercase critical still ranks 4")
}

// TestMerge_AllUnknownSeverityFallbackIsNormalized proves the all-unknown-severity
// fallback in MergeSeverity returns a normalized form, consistent with every
// known-severity path that also returns canonical uppercase.
func TestMerge_AllUnknownSeverityFallbackIsNormalized(t *testing.T) {
	group := []Finding{
		{Severity: "xyzzy", File: "a.go", Line: 1, Reviewer: "r1"},
		{Severity: "also-unknown", File: "a.go", Line: 1, Reviewer: "r2"},
	}
	eq(t, Merge(group).Severity, NormalizeSeverity(group[0].Severity), "all-unknown fallback normalized")
}

func TestMaxEstMinutes_NegativeEstimates(t *testing.T) {
	group := []Finding{
		mf("HIGH", "a.go", 1, "p", "f", "sec", -20, "e", "a"),
		mf("HIGH", "a.go", 1, "p", "f", "sec", -10, "e", "b"),
	}
	eq(t, MaxEstMinutes(group), -10, "max of negative estimates is the least negative")
}

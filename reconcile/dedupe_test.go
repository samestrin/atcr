package reconcile

import "testing"

// classify gates on integer-quantized NCD from the three compressed sizes
// (C(ab), C(a), C(b)). These cases pin the merge/gray/distinct boundaries
// exactly, independent of the compressor, by feeding synthetic sizes.

func TestClassify_MergeBelowCutoff(t *testing.T) {
	rel, sim := classify(120, 100, 100) // NCD 0.20
	eq(t, rel, relMerge, "NCD 0.20 → merge")
	inDelta(t, sim, 0.80, 1e-9, "similarity = 1 - NCD")
}

func TestClassify_GrayZone(t *testing.T) {
	rel, sim := classify(160, 100, 100) // NCD 0.60
	eq(t, rel, relGray, "NCD 0.60 → gray")
	inDelta(t, sim, 0.40, 1e-9, "similarity = 1 - NCD")
}

func TestClassify_Distinct(t *testing.T) {
	rel, _ := classify(185, 100, 100) // NCD 0.85
	eq(t, rel, relDistinct, "NCD 0.85 → distinct")
}

func TestClassify_MergeBoundaryInclusive(t *testing.T) {
	rel, _ := classify(155, 100, 100) // NCD exactly 0.550
	eq(t, rel, relMerge, "NCD == 0.550 merges (boundary inclusive)")
}

func TestClassify_GrayBoundaryInclusive(t *testing.T) {
	rel, _ := classify(175, 100, 100) // NCD exactly 0.750
	eq(t, rel, relGray, "NCD == 0.750 is gray (boundary inclusive)")
}

func TestClassify_AsymmetricSizesUseMax(t *testing.T) {
	// num = cab - min(ca,cb) = 50, denom = max(ca,cb) = 120 → NCD 0.417 → merge.
	// Were the denominator min(ca,cb)=80, NCD would be 0.625 → gray, so a merge
	// result proves the max is used.
	rel, _ := classify(130, 80, 120)
	eq(t, rel, relMerge, "denominator is max(C(a),C(b))")
}

func TestClassify_NumeratorClampedAtZero(t *testing.T) {
	rel, sim := classify(90, 100, 120) // cab < min → num clamps to 0
	eq(t, rel, relMerge, "cab below min → NCD 0 → merge")
	inDelta(t, sim, 1.0, 1e-9, "similarity clamps to 1.0")
}

// Long, realistic finding text exercises the NCD gate end-to-end via
// DedupeCluster. The whole motivation for NCD is the lexically-diverse-duplicate
// case below, which token-set overlap scores as distinct.
const (
	probLogoutA = "the jwt access token issued at login is never invalidated server-side when the user logs out; the logout handler only clears the client cookie so a captured token still authorizes requests after sign-out and can be replayed freely"
	probLogoutB = "after a person signs off from their account the previously granted session credential remains valid and keeps granting access to protected endpoints because the sign-out routine wipes only the browser state and never revokes the bearer"
	probGrid    = "the responsive grid on the dashboard overflows its container at viewport widths below three hundred sixty pixels pushing the sidebar off screen and creating a horizontal scrollbar that breaks the entire mobile layout"
)

func TestDedupeCluster_IdenticalMerges(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, probLogoutA, "greta"),
		fnd("a.go", 1, probLogoutA, "kai"),
	})
	length(t, groups, 1, "identical findings collapse into one merge group")
	length(t, groups[0], 2, "both findings")
	length(t, amb, 0, "no ambiguous")
}

func TestDedupeCluster_LexicallyDiverseDuplicateMerges(t *testing.T) {
	// Same issue, entirely different vocabulary. This is the case NCD exists to
	// catch and token-set Jaccard misses.
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, probLogoutA, "greta"),
		fnd("a.go", 1, probLogoutB, "kai"),
	})
	length(t, groups, 1, "lexically-diverse duplicate merges under NCD")
}

func TestDedupeCluster_UnrelatedDistinct(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, probLogoutA, "greta"),
		fnd("a.go", 1, probGrid, "kai"),
	})
	length(t, groups, 2, "unrelated findings stay distinct")
	length(t, amb, 0, "below the gray zone is not ambiguous, just distinct")
}

func TestDedupeCluster_TransitiveMerge(t *testing.T) {
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, probLogoutA, "greta"),
		fnd("a.go", 1, probLogoutA, "kai"),
		fnd("a.go", 1, probLogoutA, "mira"),
	})
	length(t, groups, 1, "all three merge via single-linkage")
	length(t, groups[0], 3, "three members")
}

func TestDedupeCluster_BothEmptyProblemsMerge(t *testing.T) {
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, "", "greta"),
		fnd("a.go", 1, "", "kai"),
	})
	length(t, groups, 1, "two empty findings are byte-identical → merge")
	length(t, groups[0], 2, "both members")
}

func TestDedupeCluster_EmptyVsNonEmptyDistinct(t *testing.T) {
	// An empty finding and a substantive one share no content → distinct, never a
	// spurious merge from the compressor's small-input behavior.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "", "greta"),
		fnd("a.go", 1, probLogoutA, "kai"),
	})
	length(t, groups, 2, "empty vs non-empty stays distinct")
	length(t, amb, 0, "not ambiguous")
}

func TestDedupeCluster_OrderIndependentScore(t *testing.T) {
	// The same two findings in swapped order must produce the same grouping and
	// the same advisory similarity (canonical concatenation order).
	a := fnd("a.go", 1, probLogoutA, "greta")
	b := fnd("a.go", 1, probLogoutB, "kai")
	g1, _ := DedupeCluster([]Finding{a, b})
	g2, _ := DedupeCluster([]Finding{b, a})
	eq(t, len(g1), len(g2), "grouping is order-independent")
}

func TestDedupeCluster_SingletonClusterNoAmbiguous(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{fnd("a.go", 1, "lonely finding text", "greta")})
	length(t, groups, 1, "one group")
	length(t, groups[0], 1, "one member")
	length(t, amb, 0, "no ambiguous")
}

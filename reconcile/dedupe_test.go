package reconcile

import "testing"

func TestClassifySimilarity_Identical(t *testing.T) {
	_, sim := classify(tokenize("token never expires"), tokenize("token never expires"))
	eq(t, sim, 1.0, "identical → 1.0")
}

func TestClassifySimilarity_Disjoint(t *testing.T) {
	_, sim := classify(tokenize("alpha beta"), tokenize("gamma delta"))
	eq(t, sim, 0.0, "disjoint → 0.0")
}

func TestClassifySimilarity_PartialAndCaseInsensitive(t *testing.T) {
	// {alpha,beta,gamma} vs {alpha,beta,delta}: 2 shared / 4 union = 0.5.
	_, sim := classify(tokenize("Alpha BETA gamma"), tokenize("alpha beta DELTA"))
	inDelta(t, sim, 0.5, 1e-9, "case-insensitive partial")
}

func TestClassifySimilarity_EmptyIsZero(t *testing.T) {
	_, sim := classify(tokenize(""), tokenize("anything"))
	eq(t, sim, 0.0, "empty left")
	_, sim = classify(tokenize("anything"), tokenize(""))
	eq(t, sim, 0.0, "empty right")
}

func TestDedupeCluster_HighSimilarityMerges(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "token never expires unchecked", "greta"),
		fnd("a.go", 1, "token never expires unchecked", "kai"), // identical → 1.0
	})
	length(t, groups, 1, "duplicates collapse into one merge group")
	length(t, groups[0], 2, "both findings")
	length(t, amb, 0, "no ambiguous")
}

func TestDedupeCluster_GrayZoneIsAmbiguousAndUnmerged(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "alpha beta gamma", "greta"),
		fnd("a.go", 1, "alpha beta delta", "kai"), // 0.5 → gray zone
	})
	length(t, groups, 2, "gray-zone findings stay unmerged (singleton groups)")
	length(t, amb, 1, "the pair is recorded as ambiguous")
	inDelta(t, amb[0].Similarity, 0.5, 1e-9, "similarity recorded")
	eq(t, amb[0].File, "a.go", "file recorded")
	length(t, amb[0].Findings, 2, "both members")
}

func TestDedupeCluster_LowSimilarityDistinct(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "alpha beta gamma delta", "greta"),
		fnd("a.go", 1, "epsilon zeta eta theta", "kai"), // 0.0 → distinct
	})
	length(t, groups, 2, "distinct")
	length(t, amb, 0, "below the gray zone is not ambiguous, just distinct")
}

func TestDedupeCluster_TransitiveMerge(t *testing.T) {
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, "same problem text here", "greta"),
		fnd("a.go", 1, "same problem text here", "kai"),
		fnd("a.go", 1, "same problem text here", "mira"),
	})
	length(t, groups, 1, "all three merge via single-linkage")
	length(t, groups[0], 3, "three members")
}

func TestDedupeCluster_BipartiteRefusesTransitiveOverMerge(t *testing.T) {
	// The greedy-clustering failure case (Epic 13.2 AC2). A~B is only gray (0.667)
	// — they are NOT duplicates — but A~C and B~C both clear the merge threshold
	// (0.818). Greedy single-linkage chains A-B-C into one group via C, silently
	// merging the non-duplicate pair A-B. Bipartite matching refuses this: C is one
	// finding from one source and can corroborate only ONE of A/B (1:1), so A and B
	// stay in separate groups and the unresolved A-B pair surfaces as ambiguous.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A and B 0.818 merge
	})
	length(t, groups, 2, "bipartite keeps the non-duplicate A-B apart instead of chaining all three")
	sizes := map[int]int{}
	for _, g := range groups {
		sizes[len(g)]++
	}
	eq(t, sizes[2], 1, "one corroborated pair (C joins one of A/B)")
	eq(t, sizes[1], 1, "one finding left as its own group")
	length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
}

func TestDedupeCluster_ExactThresholdBoundaries(t *testing.T) {
	// Exactly 0.7 (7 shared of 10 union) must MERGE (>= MergeThreshold).
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "a1 a2 a3 a4 a5 a6 a7 a8", "greta"),  // 8 tokens
		fnd("a.go", 1, "a1 a2 a3 a4 a5 a6 a7 b1 b2", "kai"), // 9 tokens; inter 7, union 10
	})
	length(t, groups, 1, "sim==0.7 merges (boundary inclusive)")
	length(t, amb, 0, "no ambiguous")

	// Exactly 0.4 (2 shared of 5 union) must be AMBIGUOUS (>= GrayLow, < merge).
	g2, amb2 := DedupeCluster([]Finding{
		fnd("a.go", 1, "a1 a2 a3", "greta"),  // 3 tokens
		fnd("a.go", 1, "a1 a2 b1 b2", "kai"), // 4 tokens; inter 2, union 5
	})
	length(t, g2, 2, "sim==0.4 does not merge")
	length(t, amb2, 1, "sim==0.4 is ambiguous (boundary inclusive)")
}

func TestDedupeCluster_BothEmptyProblemsMerge(t *testing.T) {
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, "", "greta"),
		fnd("a.go", 1, "", "kai"),
	})
	length(t, groups, 1, "two empty problem texts are identical → merge")
	length(t, groups[0], 2, "both members")
}

func TestDedupeCluster_SharedASTKeyMergesDisjointText(t *testing.T) {
	// Epic 13.2 composite edge weight: two cross-source findings with DISJOINT
	// problem text (Jaccard 0 → would be distinct) but a shared non-empty AST
	// GroupKey are matched at distance 0 and merge. This is the 13.1-isomorphism
	// signal acting as an edge weight, which token-set Jaccard alone cannot do.
	groups, amb := dedupeCluster([]Finding{
		fnd("a.go", 1, "alpha beta", "greta"),
		fnd("a.go", 1, "gamma delta", "kai"),
	}, []string{"a.go\x00H", "a.go\x00H"}, nil)
	length(t, groups, 1, "shared AST key merges despite disjoint text")
	length(t, groups[0], 2, "both findings")
	length(t, amb, 0, "no ambiguous: it merged")
}

func TestDedupeCluster_AdjudicatedGrayPairMerges(t *testing.T) {
	// A gray pair is left unmerged by default, but an adjudication marking its id a
	// duplicate force-merges the two groups and drops it from the sidecar. This
	// exercises the group-level union-find merge path preserved across the rewrite.
	a := fnd("a.go", 1, "alpha beta gamma", "greta")
	b := fnd("a.go", 1, "alpha beta delta", "kai") // 0.5 → gray
	id := AmbiguousID(a.File, a.Line, a.Problem, b.Problem)
	groups, amb := dedupeCluster([]Finding{a, b}, []string{"", ""}, map[string]bool{id: true})
	length(t, groups, 1, "adjudicated duplicate merges into one group")
	length(t, groups[0], 2, "both members")
	length(t, amb, 0, "an adjudicated pair is not re-recorded as ambiguous")
}

func TestClusterKeys_UsesGrouper(t *testing.T) {
	keys := clusterKeys([]Finding{fnd("a.go", 10, "x", "r1"), fnd("a.go", 20, "y", "r2")},
		fakeGrouper{keys: map[int]string{10: "K1", 20: ""}})
	eq(t, keys[0], "K1", "grouper key threaded through")
	eq(t, keys[1], "", "empty key preserved")
}

func TestDedupeCluster_SingletonClusterNoAmbiguous(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{fnd("a.go", 1, "lonely", "greta")})
	length(t, groups, 1, "one group")
	length(t, groups[0], 1, "one member")
	length(t, amb, 0, "no ambiguous")
}

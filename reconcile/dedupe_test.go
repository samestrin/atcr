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

func TestDedupeCluster_TransitivelyMergedGrayPairDropped(t *testing.T) {
	// A~B is gray (0.667) but A~C and B~C both merge (0.818), so A and B end up
	// under the same union-find root via C. The A-B pair must NOT be emitted.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A and B 0.818 merge
	})
	length(t, groups, 1, "all three collapse transitively via C")
	length(t, groups[0], 3, "three members")
	length(t, amb, 0, "a gray pair already merged transitively must not be left for adjudication")
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

func TestDedupeCluster_SingletonClusterNoAmbiguous(t *testing.T) {
	groups, amb := DedupeCluster([]Finding{fnd("a.go", 1, "lonely", "greta")})
	length(t, groups, 1, "one group")
	length(t, groups[0], 1, "one member")
	length(t, amb, 0, "no ambiguous")
}

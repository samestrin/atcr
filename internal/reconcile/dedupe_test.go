package reconcile

import (
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJaccard_Identical(t *testing.T) {
	assert.Equal(t, 1.0, jaccard("token never expires", "token never expires"))
}

func TestJaccard_Disjoint(t *testing.T) {
	assert.Equal(t, 0.0, jaccard("alpha beta", "gamma delta"))
}

func TestJaccard_PartialAndCaseInsensitive(t *testing.T) {
	// {alpha,beta,gamma} vs {alpha,beta,delta}: 2 shared / 4 union = 0.5.
	assert.InDelta(t, 0.5, jaccard("Alpha BETA gamma", "alpha beta DELTA"), 1e-9)
}

func TestJaccard_EmptyIsZero(t *testing.T) {
	assert.Equal(t, 0.0, jaccard("", "anything"))
	assert.Equal(t, 0.0, jaccard("anything", ""))
}

func TestDedupeCluster_HighSimilarityMerges(t *testing.T) {
	cluster := []stream.Finding{
		fnd("a.go", 1, "token never expires unchecked", "greta"),
		fnd("a.go", 1, "token never expires unchecked", "kai"), // identical → 1.0
	}
	groups, amb := DedupeCluster(cluster)
	require.Len(t, groups, 1, "duplicates collapse into one merge group")
	assert.Len(t, groups[0], 2)
	assert.Empty(t, amb)
}

func TestDedupeCluster_GrayZoneIsAmbiguousAndUnmerged(t *testing.T) {
	cluster := []stream.Finding{
		fnd("a.go", 1, "alpha beta gamma", "greta"),
		fnd("a.go", 1, "alpha beta delta", "kai"), // 0.5 → gray zone
	}
	groups, amb := DedupeCluster(cluster)
	assert.Len(t, groups, 2, "gray-zone findings stay unmerged (singleton groups)")
	require.Len(t, amb, 1, "the pair is recorded as ambiguous")
	assert.InDelta(t, 0.5, amb[0].Similarity, 1e-9)
	assert.Equal(t, "a.go", amb[0].File)
	assert.Len(t, amb[0].Findings, 2)
}

func TestDedupeCluster_LowSimilarityDistinct(t *testing.T) {
	cluster := []stream.Finding{
		fnd("a.go", 1, "alpha beta gamma delta", "greta"),
		fnd("a.go", 1, "epsilon zeta eta theta", "kai"), // 0.0 → distinct
	}
	groups, amb := DedupeCluster(cluster)
	assert.Len(t, groups, 2)
	assert.Empty(t, amb, "below the gray zone is not ambiguous, just distinct")
}

func TestDedupeCluster_TransitiveMerge(t *testing.T) {
	// A~B and B~C (both identical) → all three merge via single-linkage.
	cluster := []stream.Finding{
		fnd("a.go", 1, "same problem text here", "greta"),
		fnd("a.go", 1, "same problem text here", "kai"),
		fnd("a.go", 1, "same problem text here", "mira"),
	}
	groups, _ := DedupeCluster(cluster)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0], 3)
}

func TestDedupeCluster_TransitivelyMergedGrayPairDropped(t *testing.T) {
	// A~B is gray (0.667) but A~C and B~C both merge (0.818), so A and B end up
	// under the same union-find root via C. The A-B pair must NOT be emitted as
	// an AmbiguousCluster: a "merge" adjudication for it would be a silent no-op
	// and "distinct" could not be honored (the pair is already collapsed), so a
	// surviving entry would make ambiguous.json contradict findings.txt.
	cluster := []stream.Finding{
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A inter 8 / union 12 = 0.667 gray
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A and vs B inter 9 / union 11 = 0.818 merge
	}
	groups, amb := DedupeCluster(cluster)
	require.Len(t, groups, 1, "all three findings collapse transitively via C")
	assert.Len(t, groups[0], 3)
	assert.Empty(t, amb, "a gray pair already merged transitively must not be left for adjudication")
}

func TestDedupeCluster_ExactThresholdBoundaries(t *testing.T) {
	// Exactly 0.7 (7 shared of 10 union) must MERGE (>= MergeThreshold).
	merge := []stream.Finding{
		fnd("a.go", 1, "a1 a2 a3 a4 a5 a6 a7 a8", "greta"),  // 8 tokens
		fnd("a.go", 1, "a1 a2 a3 a4 a5 a6 a7 b1 b2", "kai"), // 9 tokens; inter 7, union 10
	}
	groups, amb := DedupeCluster(merge)
	assert.Len(t, groups, 1, "sim==0.7 merges (boundary inclusive)")
	assert.Empty(t, amb)

	// Exactly 0.4 (2 shared of 5 union) must be AMBIGUOUS (>= GrayLow, < merge).
	gray := []stream.Finding{
		fnd("a.go", 1, "a1 a2 a3", "greta"),  // 3 tokens
		fnd("a.go", 1, "a1 a2 b1 b2", "kai"), // 4 tokens; inter 2, union 5
	}
	g2, amb2 := DedupeCluster(gray)
	assert.Len(t, g2, 2, "sim==0.4 does not merge")
	require.Len(t, amb2, 1, "sim==0.4 is ambiguous (boundary inclusive)")
}

func TestDedupeCluster_BothEmptyProblemsMerge(t *testing.T) {
	groups, _ := DedupeCluster([]stream.Finding{
		fnd("a.go", 1, "", "greta"),
		fnd("a.go", 1, "", "kai"),
	})
	require.Len(t, groups, 1, "two empty problem texts are identical → merge")
	assert.Len(t, groups[0], 2)
}

func TestDedupeCluster_SingletonClusterNoAmbiguous(t *testing.T) {
	groups, amb := DedupeCluster([]stream.Finding{fnd("a.go", 1, "lonely", "greta")})
	require.Len(t, groups, 1)
	assert.Len(t, groups[0], 1)
	assert.Empty(t, amb)
}

package debate

import (
	"testing"

	"github.com/samestrin/atcr/internal/reconcile"
	"github.com/stretchr/testify/require"
)

// TestApplyOneClusterMerge_AnchoredFindingsMatchRawCluster guards the epic-18.1
// regression: findings.json Problem cells are symbol-anchored ("(symbol) …") by
// the reconciler, but ambiguous.json cluster members stay raw. applyOneClusterMerge
// correlates the two by File+Line+Problem, so without anchor-insensitive matching a
// judge "merge" ruling silently drops (exactHits == 0). The correlation must ignore
// the display anchor, which is not part of a finding's identity.
func TestApplyOneClusterMerge_AnchoredFindingsMatchRawCluster(t *testing.T) {
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "(classifyHeader) off by one in loop", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "(classifyHeader) loop boundary error", "HIGH", "bob"),
		grayFinding("b.go", 3, "(unrelated) different bug", "LOW", "carol"),
	}
	c := grayCluster("amb-1", "a.go", 10,
		"off by one in loop", "MEDIUM", "alice",
		"loop boundary error", "HIGH", "bob")

	out, ok := applyOneClusterMerge(findings, c)
	require.True(t, ok, "anchored findings must still match raw cluster members")
	require.Len(t, out, 2, "the two co-located members union into one; the unrelated finding is preserved")

	var merged *reconcile.JSONFinding
	for i := range out {
		if out[i].ClusterMerged {
			merged = &out[i]
		}
	}
	require.NotNil(t, merged, "a merged survivor must be produced")
	require.Equal(t, "amb-1", merged.ClusterID)
	require.Equal(t, []string{"alice", "bob"}, merged.Reviewers)
}

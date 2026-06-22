package debate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/reconcile"
)

// TestIndexClusters_CollisionUsesSentinel pins the edge case where two distinct
// co-located gray-zone clusters happen to share the same longest-member problem
// text. Without detection, indexClusters would silently overwrite one cluster
// with the other, causing a gray-zone item to resolve to the wrong cluster ID.
// The sentinel marks the display key as ambiguous so neither cluster is trusted
// for identity-keyed suppression or merge application at runtime.
func TestIndexClusters_CollisionUsesSentinel(t *testing.T) {
	c1 := grayCluster("amb-1", "a.go", 10,
		"short one", "MEDIUM", "alice",
		"shared longest problem text", "HIGH", "bob")
	c2 := grayCluster("amb-2", "a.go", 10,
		"short two", "MEDIUM", "carol",
		"shared longest problem text", "HIGH", "dave")

	idx := indexClusters([]reconcile.AmbiguousCluster{c1, c2})

	got, ok := idx[FindingKey{File: "a.go", Line: 10, Problem: "shared longest problem text"}]
	require.True(t, ok, "the shared display key must still be present")
	assert.Equal(t, collisionSentinelID, got.ID,
		"a colliding display key must resolve to the collision sentinel, not to either cluster ID")
	assert.Empty(t, got.Findings, "sentinel must carry no findings")
}

// TestFilterMergedClusters_CollisionDoesNotOverSuppress verifies that when two
// distinct clusters collide on the same display key, an already-merged cluster
// does not cause the other cluster's item to be silently dropped. The safe
// behavior is to let both items pass through (the merged one will no-op on re-
// application), eliminating the residual over-suppression gap.
func TestFilterMergedClusters_CollisionDoesNotOverSuppress(t *testing.T) {
	c1 := grayCluster("amb-1", "a.go", 10,
		"short one", "MEDIUM", "alice",
		"shared longest problem text", "HIGH", "bob")
	c2 := grayCluster("amb-2", "a.go", 10,
		"short two", "MEDIUM", "carol",
		"shared longest problem text", "HIGH", "dave")
	clusterIdx := indexClusters([]reconcile.AmbiguousCluster{c1, c2})

	items := []reconcile.DisagreementItem{
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "shared longest problem text"},
	}

	// Cluster #2 is merged; because the display key is ambiguous, the item must
	// not resolve to c2's ID and therefore must not be suppressed.
	out := filterMergedClusters(items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-2"},
	}, clusterIdx)
	require.Len(t, out, 1, "ambiguous display key must not over-suppress the unmerged cluster")
	assert.Equal(t, "shared longest problem text", out[0].Problem)
}

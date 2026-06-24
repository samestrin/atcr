package debate

import (
	"bytes"
	"context"
	reclib "github.com/samestrin/atcr/reconcile"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/log"
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

	idx := indexClusters([]reclib.AmbiguousCluster{c1, c2})

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
	clusterIdx := indexClusters([]reclib.AmbiguousCluster{c1, c2})

	items := []reconcile.DisagreementItem{
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "shared longest problem text"},
	}

	// Cluster #2 is merged; because the display key is ambiguous, the item must
	// not resolve to c2's ID and therefore must not be suppressed.
	out := filterMergedClusters(context.Background(), items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "merged survivor", ClusterMerged: true, ClusterID: "amb-2"},
	}, clusterIdx)
	require.Len(t, out, 1, "ambiguous display key must not over-suppress the unmerged cluster")
	assert.Equal(t, "shared longest problem text", out[0].Problem)
}

// TestFilterMergedClusters_LocationFallbackSuppressesDriftedProblem pins the
// drift case: a gray-zone cluster was merged in a prior run, but the radar item's
// representative problem no longer matches the cluster's display key (e.g. the
// reconcile and debate implementations drifted, or the merged survivor's problem
// was rewritten). Without a fallback, the item silently passes through and is
// re-debated. The location-keyed fallback suppresses it when a merged survivor
// with a non-empty ClusterID sits at the same File+Line.
func TestFilterMergedClusters_LocationFallbackSuppressesDriftedProblem(t *testing.T) {
	c := grayCluster("amb-1", "a.go", 10,
		"alpha problem text", "MEDIUM", "alice",
		"beta problem text", "HIGH", "bob")
	clusterIdx := indexClusters([]reclib.AmbiguousCluster{c})

	items := []reconcile.DisagreementItem{
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "drifted representative problem"},
	}

	out := filterMergedClusters(context.Background(), items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "beta problem text", ClusterMerged: true, ClusterID: "amb-1"},
	}, clusterIdx)
	require.Empty(t, out, "a drifted gray-zone item at a merged location must be suppressed by location fallback")
}

// TestApplyOneClusterMerge_EmptyClusterIDIsNoOp pins the construction invariant
// that a gray-zone cluster always carries a stable AmbiguousCluster.ID (a non-
// empty sha256 hex). A cluster with a blank ID — reachable only via a hand-edited
// or corrupt ambiguous.json — must NOT produce a ClusterMerged survivor: such a
// survivor carries an empty ClusterID, which filterMergedClusters treats as a
// legacy record and never suppresses, so the cluster would be re-debated every
// run with no way to self-heal. applyOneClusterMerge must refuse the merge.
func TestApplyOneClusterMerge_EmptyClusterIDIsNoOp(t *testing.T) {
	c := grayCluster("", "a.go", 10,
		"alpha problem text", "MEDIUM", "alice",
		"beta problem text", "HIGH", "bob")
	findings := []reconcile.JSONFinding{
		grayFinding("a.go", 10, "alpha problem text", "MEDIUM", "alice"),
		grayFinding("a.go", 10, "beta problem text", "HIGH", "bob"),
	}

	out, ok := applyOneClusterMerge(findings, c)
	require.False(t, ok, "a cluster with an empty ID must not be applied")
	require.Len(t, out, 2, "no records may be unioned when the merge is refused")
	for _, f := range out {
		assert.False(t, f.ClusterMerged, "no survivor may be flagged cluster_merged")
		assert.Empty(t, f.ClusterID, "no survivor may carry a ClusterID")
	}
}

// TestFilterMergedClusters_LogsSuppressedCount pins the observability contract:
// when filterMergedClusters drops already-merged gray-zone items, it emits a
// debug log carrying the suppressed count so an idempotency mis-fire (wrong-ID
// match or unexpected pass-through) is visible in prod rather than silent.
func TestFilterMergedClusters_LogsSuppressedCount(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := log.NewContext(context.Background(), logger)

	c := grayCluster("amb-1", "a.go", 10,
		"alpha problem text", "MEDIUM", "alice",
		"beta problem text", "HIGH", "bob")
	clusterIdx := indexClusters([]reclib.AmbiguousCluster{c})
	items := []reconcile.DisagreementItem{
		{Kind: reconcile.KindGrayZone, File: "a.go", Line: 10, Problem: "drifted representative problem"},
	}

	out := filterMergedClusters(ctx, items, []reconcile.JSONFinding{
		{File: "a.go", Line: 10, Problem: "beta problem text", ClusterMerged: true, ClusterID: "amb-1"},
	}, clusterIdx)
	require.Empty(t, out, "the merged item must be suppressed by the location fallback")
	assert.Contains(t, buf.String(), "suppressed=1", "a debug log must record the suppressed-item count")
}

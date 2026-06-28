package reconcile

import "testing"

// adjacency builds a symmetric neighbor predicate from an edge list (pairs of
// indices that are within eps of each other).
func adjacency(edges [][2]int) func(i, j int) bool {
	set := map[[2]int]bool{}
	for _, e := range edges {
		set[[2]int{e[0], e[1]}] = true
		set[[2]int{e[1], e[0]}] = true
	}
	return func(i, j int) bool { return set[[2]int{i, j}] }
}

func TestDBSCAN_IsolatesNoisePoint(t *testing.T) {
	// 0-1 are neighbors (dense), 2 is isolated. minPts=2 → {0,1} cluster, 2 noise.
	labels, dense := dbscanLabels(3, 2, adjacency([][2]int{{0, 1}}))
	isTrue(t, dense, "a dense cluster formed")
	eq(t, labels[0], labels[1], "0 and 1 share a cluster")
	notEq(t, labels[0], dbscanNoise, "0 is not noise")
	eq(t, labels[2], dbscanNoise, "2 is isolated noise")
}

func TestDBSCAN_AllIsolatedNoDenseCluster(t *testing.T) {
	labels, dense := dbscanLabels(3, 2, adjacency(nil))
	isTrue(t, !dense, "no cluster formed when every point is isolated")
	for i := 0; i < 3; i++ {
		eq(t, labels[i], dbscanNoise, "every point is noise")
	}
}

func TestDBSCAN_DensityConnectedChain(t *testing.T) {
	// 0-1 and 1-2 are neighbors but 0-2 are not. Density-reachability links all
	// three into one cluster (no point is noise).
	labels, dense := dbscanLabels(3, 2, adjacency([][2]int{{0, 1}, {1, 2}}))
	isTrue(t, dense, "dense")
	eq(t, labels[0], labels[1], "0,1 same cluster")
	eq(t, labels[1], labels[2], "1,2 same cluster → all connected")
}

func TestDBSCAN_MinPtsThreeLeavesPairAsNoise(t *testing.T) {
	// With minPts=3 a bare pair (each has only 1 neighbor) is not dense → noise.
	labels, dense := dbscanLabels(2, 3, adjacency([][2]int{{0, 1}}))
	isTrue(t, !dense, "a pair is not dense at minPts=3")
	eq(t, labels[0], dbscanNoise, "0 noise")
	eq(t, labels[1], dbscanNoise, "1 noise")
}

func TestDBSCAN_NeighborComputedOncePerPair(t *testing.T) {
	// Single-pass invariant: each point's eps-neighborhood is computed exactly
	// once, so the predicate is evaluated n*(n-1) times total (once per ordered
	// pair) regardless of cluster topology — never re-scanned during expansion.
	// This is the inherent O(n^2) cost of naive DBSCAN; there is no repeated scan
	// to cache away, and an explicit adjacency list would only add O(n^2) resident
	// memory for no CPU win. Guards against a regression that re-scans a point.
	for _, tc := range []struct {
		name  string
		edges [][2]int
	}{
		{"chain", [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}}},
		{"star", [][2]int{{0, 1}, {0, 2}, {0, 3}, {0, 4}, {0, 5}}},
		{"isolated", nil},
	} {
		const n = 6
		base := adjacency(tc.edges)
		calls := 0
		counting := func(i, j int) bool { calls++; return base(i, j) }
		dbscanLabels(n, 2, counting)
		eq(t, calls, n*(n-1), tc.name+": neighbor predicate evaluated exactly once per ordered pair")
	}
}

func TestDBSCAN_Deterministic(t *testing.T) {
	edges := [][2]int{{0, 1}, {2, 3}, {3, 4}}
	l1, _ := dbscanLabels(6, 2, adjacency(edges))
	l2, _ := dbscanLabels(6, 2, adjacency(edges))
	for i := range l1 {
		eq(t, l1[i], l2[i], "stable labels across runs")
	}
	eq(t, l1[5], dbscanNoise, "point 5 has no edges → noise")
}

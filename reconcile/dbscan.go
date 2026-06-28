package reconcile

// DBSCAN noise isolation for Epic 13.2. After bipartite matching forms the merge
// groups, DBSCAN over the same location cluster separates uncorroborated "noise"
// findings (single-model hallucinations) from the dense, corroborated consensus —
// deterministically and without an arbitrary cluster count k. The two parameters
// are principled, not tuned: minPts = 2 ("corroborated by at least one other
// finding") and the eps-neighborhood is the merge boundary itself, supplied as an
// integer-exact predicate so the density threshold never drifts on floating-point
// rounding (the same boundary bipartite acceptance uses).
const (
	// dbscanMinPts is the core-point threshold: a point plus one eps-neighbor.
	dbscanMinPts = 2
	// dbscanNoise labels a point DBSCAN could not assign to any cluster.
	dbscanNoise = -1
	// dbscanUnvisited is the initial label before a point is processed.
	dbscanUnvisited = -2
)

// dbscanLabels runs DBSCAN over n points whose eps-neighborhood is defined by the
// neighbor predicate — neighbor(i, j) reports whether j (j != i) lies within eps
// of i and must be symmetric. It returns a label per point (dbscanNoise for noise,
// otherwise a 0-based cluster id) and dense=true when at least one cluster formed.
//
// Determinism: points are visited in index order; a cluster's seed set is grown
// as a FIFO with each expansion's neighbors appended in ascending index order, so
// the labeling is identical on every run and platform.
//
// Single pass: each point's neighborhood is computed exactly once. A point pulled
// into a cluster by expansion is labeled before the outer loop reaches it (and a
// noise point is labeled before any expansion can re-enqueue it), so neighbor is
// evaluated once per ordered pair — n*(n-1) total. That is the inherent O(n^2)
// cost of naive DBSCAN with no spatial index; there is no repeated scan, so an
// explicit precomputed adjacency list would only add O(n^2) resident memory for
// no CPU reduction (see TestDBSCAN_NeighborComputedOncePerPair).
func dbscanLabels(n, minPts int, neighbor func(i, j int) bool) (labels []int, dense bool) {
	labels = make([]int, n)
	for i := range labels {
		labels[i] = dbscanUnvisited
	}
	queued := make([]bool, n)
	neighborsOf := func(p int) []int {
		var out []int
		for q := 0; q < n; q++ {
			if q != p && neighbor(p, q) {
				out = append(out, q)
			}
		}
		return out
	}
	cluster := 0
	for p := 0; p < n; p++ {
		if labels[p] != dbscanUnvisited {
			continue
		}
		nb := neighborsOf(p)
		if len(nb)+1 < minPts { // +1 counts p itself toward the core threshold
			labels[p] = dbscanNoise
			continue
		}
		labels[p] = cluster
		seeds := append([]int(nil), nb...)
		for _, q := range seeds {
			queued[q] = true
		}
		for k := 0; k < len(seeds); k++ {
			q := seeds[k]
			if labels[q] == dbscanNoise {
				labels[q] = cluster // a former-noise point reachable from a core is a border point
			}
			if labels[q] != dbscanUnvisited {
				continue
			}
			labels[q] = cluster
			qn := neighborsOf(q)
			if len(qn)+1 >= minPts {
				for _, r := range qn {
					if labels[r] == dbscanUnvisited && !queued[r] {
						queued[r] = true
						seeds = append(seeds, r)
					}
				}
			}
		}
		cluster++
	}
	return labels, cluster > 0
}

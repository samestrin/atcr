package reconcile

// Composite edge-weight distance for Epic 13.2 bipartite matching / DBSCAN.
//
// The dependency note for 13.2 requires the 13.1 AST-isomorphism metric to be
// usable as an edge weight (13.0 NCD was falsified and not adopted). The signal
// is therefore composite and per-pair:
//
//   - distance 0 when two findings share the same non-empty AST GroupKey: they
//     sit at structurally-isomorphic code, the strongest available duplicate
//     signal.
//   - 1 - token-set Jaccard otherwise (proximity-grouped or unkeyed findings),
//     reusing the integer-authoritative similarity classify already computes so
//     the distance is deterministic and stdlib-only.
//
// distanceFloor / distanceCeiling bound the metric to [0,1]. The merge and
// gray-zone distance cutoffs (1 - MergeThreshold, 1 - GrayLow) live with the
// matching and DBSCAN passes that consume them.
const (
	distanceFloor   = 0.0
	distanceCeiling = 1.0
)

// pairDistance returns the composite edge-weight distance between two findings,
// given their precomputed AST group keys and PROBLEM-text token sets. Token sets
// are precomputed by the caller so the O(V^2) matrix build stays cheap. The
// computation is symmetric in its arguments.
func pairDistance(keyA, keyB string, tokA, tokB map[string]struct{}) float64 {
	if keyA != "" && keyA == keyB {
		return distanceFloor
	}
	_, sim := classify(tokA, tokB)
	return distanceCeiling - sim
}

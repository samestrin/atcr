package reconcile

import "sort"

// PageRank parameters (epic 13.3). The damping factor is the standard 0.85.
// Convergence is driven by the L1 epsilon: the power method's error contracts at
// ~damping per step, so reaching epsilon=1e-12 takes on the order of 170
// iterations — the iteration cap is therefore a strict backstop set comfortably
// above that (so the early-exit, not the cap, normally ends the loop) yet still
// bounds a pathological graph from spinning (the epic's convergence risk). All
// three are fixed — the signal must be deterministic per run.
const (
	pageRankDamping = 0.85
	pageRankMaxIter = 1000
	pageRankEpsilon = 1e-12
)

// agreementGraph is the run-global model-agreement graph (epic 13.3): nodes are
// models (reviewers), an edge between two models is count-weighted by how many
// findings they agreed on across the whole reconcile run. It is undirected — an
// agreement is mutual — and stored as a symmetric adjacency map. Authority
// (PageRank over this graph) is therefore a property of the whole run, not of any
// single cluster.
type agreementGraph struct {
	// adj[u][v] == adj[v][u] is the number of findings models u and v agreed on.
	adj map[string]map[string]int
}

// newAgreementGraph returns an empty graph ready for addAgreement.
func newAgreementGraph() *agreementGraph {
	return &agreementGraph{adj: map[string]map[string]int{}}
}

// addAgreement records that every distinct pair of the given reviewers agreed on
// one finding: each unordered pair's edge weight is incremented by one. Empty
// reviewer names and self-pairs are ignored, and duplicate names within the slice
// are collapsed, so a single model repeating itself never forges an edge. A slice
// with fewer than two distinct non-empty reviewers adds nothing (no agreement).
func (g *agreementGraph) addAgreement(reviewers []string) {
	seen := make(map[string]bool, len(reviewers))
	distinct := make([]string, 0, len(reviewers))
	for _, r := range reviewers {
		if r == "" || seen[r] {
			continue
		}
		seen[r] = true
		distinct = append(distinct, r)
	}
	sort.Strings(distinct)
	for i := 0; i < len(distinct); i++ {
		for j := i + 1; j < len(distinct); j++ {
			g.addEdge(distinct[i], distinct[j])
		}
	}
}

// addEdge increments the symmetric count-weighted edge between u and v.
func (g *agreementGraph) addEdge(u, v string) {
	if g.adj[u] == nil {
		g.adj[u] = map[string]int{}
	}
	if g.adj[v] == nil {
		g.adj[v] = map[string]int{}
	}
	g.adj[u][v]++
	g.adj[v][u]++
}

// nodes returns the sorted node list. Sorting fixes the iteration order so the
// power iteration's float accumulation is byte-identical across runs.
func (g *agreementGraph) nodes() []string {
	out := make([]string, 0, len(g.adj))
	for u := range g.adj {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// pageRank runs deterministic weighted PageRank over the agreement graph and
// returns each model's authority score. Scores sum to 1. An empty graph returns
// an empty map.
//
// The iteration is the standard damped power method: rank flows along edges in
// proportion to their weight share of the source node's total out-weight, with a
// (1-d)/N teleport term and uniform redistribution of any dangling mass. Node and
// neighbor traversal both follow sorted order, and the iteration cap guarantees
// termination — together these make the result reproducible per run.
func (g *agreementGraph) pageRank() map[string]float64 {
	nodes := g.nodes()
	n := len(nodes)
	if n == 0 {
		return map[string]float64{}
	}

	// Precompute each node's total out-weight and its sorted neighbor list so the
	// inner accumulation order is fixed.
	outWeight := make(map[string]float64, n)
	neighbors := make(map[string][]string, n)
	for _, u := range nodes {
		w := 0.0
		nbrs := make([]string, 0, len(g.adj[u]))
		for v, c := range g.adj[u] {
			w += float64(c)
			nbrs = append(nbrs, v)
		}
		sort.Strings(nbrs)
		outWeight[u] = w
		neighbors[u] = nbrs
	}

	rank := make(map[string]float64, n)
	for _, u := range nodes {
		rank[u] = 1.0 / float64(n)
	}

	teleport := (1.0 - pageRankDamping) / float64(n)
	next := make(map[string]float64, n)
	for iter := 0; iter < pageRankMaxIter; iter++ {
		// Dangling mass (nodes with no out-edges) is redistributed uniformly so the
		// total rank stays conserved at 1. This branch is defensive-only for the
		// agreement graph: addEdge always creates symmetric edges, so every node
		// has outWeight >= 1 and dangling remains 0.
		dangling := 0.0
		for _, u := range nodes {
			if outWeight[u] == 0 {
				dangling += rank[u]
			}
		}
		danglingShare := pageRankDamping * dangling / float64(n)

		delta := 0.0
		for _, u := range nodes {
			inflow := 0.0
			for _, v := range neighbors[u] {
				if ow := outWeight[v]; ow > 0 {
					inflow += float64(g.adj[v][u]) / ow * rank[v]
				}
			}
			nv := teleport + danglingShare + pageRankDamping*inflow
			next[u] = nv
			d := nv - rank[u]
			if d < 0 {
				d = -d
			}
			delta += d
		}
		rank, next = next, rank
		if delta < pageRankEpsilon {
			break
		}
	}
	return rank
}

// modelAuthority builds the run-global agreement graph from every merge group and
// returns each model's PageRank authority. A group corroborated by two or more
// distinct reviewers is an agreement; single-reviewer (isolated) and unattributed
// groups add no edges. When no agreement exists anywhere in the run the result is
// empty — the backward-compatible signal that disables authority promotion, so
// confidence stays byte-identical to the pre-13.3 vote-count behavior.
func modelAuthority(groups [][]Finding) map[string]float64 {
	g := newAgreementGraph()
	any := false
	for _, group := range groups {
		revs := distinctReviewers(group)
		if len(revs) >= 2 {
			g.addAgreement(revs)
			any = true
		}
	}
	if !any {
		return map[string]float64{}
	}
	return g.pageRank()
}

// promoteByAuthority raises an isolated finding's confidence from MEDIUM to HIGH
// when its sole reviewer's run authority exceeds the uniform 1/N baseline (epic
// 13.3, AC3). It is promote-only: a finding is never demoted, so a valid isolated
// finding can never be silently dropped below the downstream HIGH gate. Findings
// with two or more reviewers (already HIGH) and findings whose model sits at or
// below the baseline pass through unchanged, and an empty authority map (no
// agreement in the run) is a no-op — together these keep the pre-13.3 confidence
// exactly when no model has earned differential authority.
func promoteByAuthority(m Merged, authority map[string]float64) Merged {
	if len(authority) == 0 || len(m.Reviewers) != 1 || m.Confidence != ConfMedium {
		return m
	}
	baseline := 1.0 / float64(len(authority))
	// The strict > baseline comparison is intentional and exact for
	// vertex-transitive agreement graphs. Non-vertex-transitive nodes landing
	// infinitesimally above 1/N due to float truncation stay at MEDIUM, per the
	// epic clarification that the threshold should not be relaxed.
	if authority[m.Reviewers[0]] > baseline {
		m.Confidence = ConfHigh
	}
	return m
}

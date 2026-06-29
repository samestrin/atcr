package reconcile

import "testing"

// grp builds a merge group (slice of co-located findings) from a list of
// reviewer names, one finding per reviewer. Only the Reviewer field matters for
// authority graph construction.
func grp(reviewers ...string) []Finding {
	out := make([]Finding, len(reviewers))
	for i, r := range reviewers {
		out[i] = Finding{Reviewer: r}
	}
	return out
}

func TestModelAuthority_NoGroupsIsEmpty(t *testing.T) {
	eq(t, len(modelAuthority(nil)), 0, "nil groups → no authority")
	eq(t, len(modelAuthority([][]Finding{})), 0, "empty groups → no authority")
}

func TestModelAuthority_AllSingletonsHaveNoGraph(t *testing.T) {
	// A run where no two models ever agreed produces no edges → empty authority,
	// which is the backward-compat signal (no promotions happen downstream).
	groups := [][]Finding{grp("alpha"), grp("beta"), grp("gamma")}
	eq(t, len(modelAuthority(groups)), 0, "no agreement → no authority nodes")
}

func TestModelAuthority_SelfDuplicateIsNotAgreement(t *testing.T) {
	// Two findings from the SAME reviewer in one group is one model repeating
	// itself, not cross-model agreement: it must add no edge and no node.
	groups := [][]Finding{grp("alpha", "alpha")}
	eq(t, len(modelAuthority(groups)), 0, "self-duplicate is not agreement")
}

func TestModelAuthority_KeyedByReviewer(t *testing.T) {
	groups := [][]Finding{grp("alpha", "beta")}
	auth := modelAuthority(groups)
	eq(t, len(auth), 2, "two agreeing models → two authority nodes")
	_, hasA := auth["alpha"]
	_, hasB := auth["beta"]
	isTrue(t, hasA && hasB, "authority keyed by reviewer name")
}

func TestModelAuthority_EmptyReviewerCarriesNoAuthority(t *testing.T) {
	// Unattributed findings (empty Reviewer) must not become a graph node.
	groups := [][]Finding{grp("alpha", "")}
	eq(t, len(modelAuthority(groups)), 0, "empty reviewer is not a node")
}

func TestAgreementGraph_SymmetricPairSplitsEvenly(t *testing.T) {
	g := newAgreementGraph()
	g.addAgreement([]string{"alpha", "beta"})
	pr := g.pageRank()
	inDelta(t, pr["alpha"], 0.5, 1e-6, "symmetric pair → 0.5 each")
	inDelta(t, pr["beta"], 0.5, 1e-6, "symmetric pair → 0.5 each")
	// Neither strictly exceeds the 1/N baseline (0.5): a regular graph yields no
	// promotion, which protects existing 2-reviewer fixtures.
	isTrue(t, pr["alpha"] <= 0.5, "symmetric node does not exceed baseline")
}

func TestAgreementGraph_StarCenterOutranksLeaves(t *testing.T) {
	// alpha agrees with both beta and gamma; beta and gamma never agree with
	// each other. alpha is the central, authoritative model.
	g := newAgreementGraph()
	g.addAgreement([]string{"alpha", "beta"})
	g.addAgreement([]string{"alpha", "gamma"})
	pr := g.pageRank()
	inDelta(t, pr["alpha"], 0.486486, 1e-5, "star center PageRank")
	inDelta(t, pr["beta"], 0.256757, 1e-5, "star leaf PageRank")
	inDelta(t, pr["gamma"], 0.256757, 1e-5, "star leaf PageRank")
	baseline := 1.0 / 3.0
	isTrue(t, pr["alpha"] > baseline, "center exceeds 1/N baseline")
	isTrue(t, pr["beta"] < baseline, "leaf below 1/N baseline")
}

func TestAgreementGraph_CompleteGraphIsUniform(t *testing.T) {
	// Every model agrees with every other → fully symmetric → uniform authority.
	g := newAgreementGraph()
	g.addAgreement([]string{"alpha", "beta", "gamma"})
	pr := g.pageRank()
	third := 1.0 / 3.0
	inDelta(t, pr["alpha"], third, 1e-6, "K3 uniform")
	inDelta(t, pr["beta"], third, 1e-6, "K3 uniform")
	inDelta(t, pr["gamma"], third, 1e-6, "K3 uniform")
}

func TestAgreementGraph_SumsToOne(t *testing.T) {
	g := newAgreementGraph()
	g.addAgreement([]string{"alpha", "beta"})
	g.addAgreement([]string{"alpha", "gamma"})
	g.addAgreement([]string{"beta", "delta"})
	pr := g.pageRank()
	sum := 0.0
	for _, v := range pr {
		sum += v
	}
	inDelta(t, sum, 1.0, 1e-6, "PageRank distribution sums to 1")
}

func TestAgreementGraph_CountWeightAffectsRank(t *testing.T) {
	// alpha agreed with beta twice but gamma once. beta's stronger tie to the
	// central node earns it more inherited authority than gamma.
	g := newAgreementGraph()
	g.addAgreement([]string{"alpha", "beta"})
	g.addAgreement([]string{"alpha", "beta"})
	g.addAgreement([]string{"alpha", "gamma"})
	pr := g.pageRank()
	isTrue(t, pr["alpha"] > pr["beta"], "central node ranks highest")
	isTrue(t, pr["beta"] > pr["gamma"], "stronger tie outranks weaker tie")
}

func TestAgreementGraph_Deterministic(t *testing.T) {
	build := func() map[string]float64 {
		g := newAgreementGraph()
		g.addAgreement([]string{"gamma", "alpha"})
		g.addAgreement([]string{"alpha", "beta"})
		g.addAgreement([]string{"delta", "alpha"})
		g.addAgreement([]string{"beta", "gamma"})
		return g.pageRank()
	}
	deepEq(t, build(), build(), "PageRank is byte-identical across runs")
}

func TestAgreementGraph_EmptyHasNoRanks(t *testing.T) {
	eq(t, len(newAgreementGraph().pageRank()), 0, "empty graph → no ranks")
}

// BenchmarkModelAuthority characterizes the epic's <5ms NFR with headroom: a 24-model
// run with thousands of agreement groups (far beyond a realistic handful of
// reviewers) still builds the graph and runs PageRank well under the bound.
func BenchmarkModelAuthority(b *testing.B) {
	models := make([]string, 24)
	for i := range models {
		models[i] = "model-" + string(rune('a'+i))
	}
	var groups [][]Finding
	for g := 0; g < 4000; g++ {
		a := models[g%len(models)]
		c := models[(g*7+3)%len(models)]
		groups = append(groups, grp(a, c))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = modelAuthority(groups)
	}
}

func TestAgreementGraph_AddAgreementSkipsEmptyAndDuplicates(t *testing.T) {
	// Defensive contract: empty names and within-slice duplicates never forge an
	// edge or a spurious node. ["", "alpha", "alpha", "beta"] has exactly two
	// distinct real models, so it is one alpha-beta agreement and nothing else.
	g := newAgreementGraph()
	g.addAgreement([]string{"", "alpha", "alpha", "beta"})
	eq(t, len(g.adj), 2, "only the two real models become nodes")
	eq(t, g.adj["alpha"]["beta"], 1, "exactly one alpha-beta edge")
	_, hasEmpty := g.adj[""]
	isTrue(t, !hasEmpty, "empty reviewer never becomes a node")

	// A slice with only one distinct real model adds no edge at all.
	g2 := newAgreementGraph()
	g2.addAgreement([]string{"alpha", "", "alpha"})
	eq(t, len(g2.adj), 0, "single distinct model → no agreement edge")
}

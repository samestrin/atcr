package reconcile

import "testing"

// Epic 13.2 AC3: DBSCAN deterministically isolates known model hallucinations
// into the ambiguous sidecar — single-finding AmbiguousClusters — without an
// arbitrary cluster-count k, and only where corroboration context exists.

func TestAC3_IsolatesHallucinationAmidConsensus(t *testing.T) {
	// greta and kai corroborate one issue (identical text → merge); mira reports
	// an unrelated finding at the same location that no one else saw. The
	// corroborated pair is the dense cluster; mira's lone finding is isolated as
	// noise into the sidecar and removed from the merged output.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "token never expires unchecked here", "greta"),
		fnd("a.go", 1, "token never expires unchecked here", "kai"),
		fnd("a.go", 1, "completely different spurious claim", "mira"), // hallucination
	})
	length(t, groups, 1, "only the corroborated consensus pair remains in the output")
	length(t, groups[0], 2, "greta + kai")
	length(t, amb, 1, "the hallucination is isolated into the sidecar")
	length(t, amb[0].Findings, 1, "single-finding noise cluster")
	eq(t, amb[0].Findings[0].Reviewer, "mira", "the lone model's finding")
	eq(t, amb[0].Similarity, 0.0, "no corroboration → similarity 0")
}

func TestAC3_SoloFindingIsNotIsolated(t *testing.T) {
	// A single finding at a location no one else touched has no corroboration
	// context, so it is NOT noise — it stays a normal (uncorroborated) finding.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 5, "unused import lingers", "greta"),
	})
	length(t, groups, 1, "solo finding stays in the output")
	length(t, groups[0], 1, "the finding itself")
	length(t, amb, 0, "nothing isolated without a dense cluster for context")
}

func TestAC3_AllDistinctNoIsolation(t *testing.T) {
	// Three mutually-distinct findings: no dense cluster forms, so none is labeled
	// a hallucination — they are independent findings, not noise.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "alpha beta gamma", "greta"),
		fnd("a.go", 1, "delta epsilon zeta", "kai"),
		fnd("a.go", 1, "eta theta iota", "mira"),
	})
	length(t, groups, 3, "all three remain as independent findings")
	length(t, amb, 0, "no corroboration context → no isolation")
}

func TestAC3_GrayPairedFindingNotDoubleCountedAsNoise(t *testing.T) {
	// A dense pair (greta+kai merge) plus a third finding that is GRAY to the pair
	// (sim in [0.4,0.7)). The gray finding is recorded as a gray pair, not also as
	// a noise singleton — exactly one sidecar entry references it.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "alpha beta gamma delta epsilon", "greta"),
		fnd("a.go", 1, "alpha beta gamma delta epsilon", "kai"), // merge with greta
		fnd("a.go", 1, "alpha beta gamma zeta eta", "mira"),     // 3/7 ~0.43 gray to the pair
	})
	// The gray third stays as its own uncorroborated finding (not merged, not
	// isolated as noise): the consensus pair plus mira's singleton.
	length(t, groups, 2, "consensus pair + mira's gray singleton")
	// Every sidecar entry mentioning mira must be a 2-finding gray pair, never a
	// 1-finding noise cluster (no double representation).
	sawMira := false
	for _, c := range amb {
		for _, f := range c.Findings {
			if f.Reviewer == "mira" {
				sawMira = true
				length(t, c.Findings, 2, "mira appears only in the gray pair, not as noise")
			}
		}
	}
	isTrue(t, sawMira, "mira is referenced in the gray sidecar")
}

func TestAC3_AdjudicatedMergeNotDoubleCountedAsNoise(t *testing.T) {
	// Guard against double representation: A-B are gray and adjudicated as a merge,
	// so they form one group even though neither has a true (mergeable) neighbor. A
	// separate dense pair D-E makes the cluster "dense", so DBSCAN would label A and
	// B noise — but because they were merged (not solo), they must NOT be isolated
	// into the sidecar. Every finding ends up in exactly one place.
	a := fnd("x.go", 1, "alpha beta gamma", "mira")
	b := fnd("x.go", 1, "alpha beta delta", "pool") // gray to A (0.5)
	id := AmbiguousID(a.File, a.Line, a.Problem, b.Problem)
	groups, amb := dedupeCluster([]Finding{
		a, b,
		fnd("x.go", 1, "zeta eta theta iota kappa", "greta"),
		fnd("x.go", 1, "zeta eta theta iota kappa", "kai"), // identical → dense pair
	}, make([]string, 4), map[string]bool{id: true})
	length(t, groups, 2, "adjudicated A-B pair and the D-E consensus pair")
	for _, g := range groups {
		length(t, g, 2, "each group has two members; nothing was wrongly isolated")
	}
	length(t, amb, 0, "no noise singletons: A and B were merged, not isolated")
}

func TestAC3_SelfDuplicateIsNotCorroboration(t *testing.T) {
	// Density must require TWO distinct sources. One reviewer (greta) repeating
	// itself is NOT corroboration, so it must NOT manufacture the dense-context
	// that would banish a different reviewer's legitimate uncorroborated finding.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "token never expires unchecked here", "greta"),
		fnd("a.go", 1, "token never expires unchecked here", "greta"), // SAME source self-dup
		fnd("a.go", 1, "an entirely different real issue", "host"),    // host's lone finding
	})
	// No cross-source agreement exists, so nothing is isolated; host's finding
	// stays in the output rather than being wrongly removed to the sidecar.
	length(t, amb, 0, "a self-duplicate is not corroboration → no isolation")
	stillPresent := false
	for _, g := range groups {
		for _, f := range g {
			if f.Reviewer == "host" {
				stillPresent = true
			}
		}
	}
	isTrue(t, stillPresent, "host's uncorroborated finding stays in the consensus output")
}

func TestAC3_UnattributedCopiesAreNotCrossSourceCorroboration(t *testing.T) {
	// Two empty-Reviewer copies of the same finding are the SAME (unknown) source,
	// not two independent corroborating sources. srcKeys gives each its own key for
	// bipartite MATCHING, but the DENSITY predicate must collapse them: otherwise
	// two unattributed copies of a spurious claim manufacture a dense cluster that
	// wrongly isolates a different, legitimate finding as noise.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "spurious duplicated claim text", ""),           // unattributed copy 1
		fnd("a.go", 1, "spurious duplicated claim text", ""),           // unattributed copy 2 (merges with copy 1)
		fnd("a.go", 1, "a completely different real finding", "greta"), // legitimate, distinct
	})
	length(t, amb, 0, "two unattributed copies are not corroboration → nothing isolated as noise")
	stillPresent := false
	for _, g := range groups {
		for _, f := range g {
			if f.Reviewer == "greta" {
				stillPresent = true
			}
		}
	}
	isTrue(t, stillPresent, "the legitimate finding stays in the output")
}

func TestDedupeCluster_SameSourceDuplicatesDoNotMerge(t *testing.T) {
	// Documented behavior of the cross-source matching model: two identical
	// findings from the SAME reviewer are not merged with each other (matching is
	// 1:1 across sources, never within a source). They remain separate findings.
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, "token never expires unchecked", "greta"),
		fnd("a.go", 1, "token never expires unchecked", "greta"),
	})
	length(t, groups, 2, "same-source duplicates stay as separate findings")
}

func TestDedupeCluster_UnattributedDuplicatesStillMerge(t *testing.T) {
	// Findings with no Reviewer are each treated as their own source, so genuine
	// duplicates still merge instead of collapsing into one non-matchable
	// pseudo-source (which would silently disable dedup).
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, "token never expires unchecked", ""),
		fnd("a.go", 1, "token never expires unchecked", ""),
	})
	length(t, groups, 1, "unattributed duplicates merge")
	length(t, groups[0], 2, "both members")
}

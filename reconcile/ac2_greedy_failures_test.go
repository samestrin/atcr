package reconcile

import (
	"sort"
	"testing"
)

// Epic 13.2 AC2 benchmark: bipartite matching resolves known greedy-clustering
// (single-linkage union-find) failure edge cases. Each case documents what the
// old greedy partitioner produced and asserts the bipartite partitioner's
// corrected grouping. The shared failure mode is the same: single-linkage takes
// the transitive closure of the "similar enough" graph, so one finding can drag
// several non-duplicates into a single group. Optimal 1:1 assignment cannot.

// groupSizes returns the sorted multiset of group sizes for a dedupe result, a
// permutation-independent fingerprint of the partition.
func groupSizes(groups [][]Finding) []int {
	sizes := make([]int, len(groups))
	for i, g := range groups {
		sizes[i] = len(g)
	}
	sort.Ints(sizes)
	return sizes
}

func TestAC2_SingleSourceFindingDoesNotAbsorbTwoFromAnother(t *testing.T) {
	// One reviewer (greta) reports one issue; another (kai) reports TWO findings
	// that each clear the merge threshold against greta's. Greedy union-find
	// merges A~P and A~Q and collapses all three into ONE group — double-counting
	// kai and inventing 3-way consensus from a 1+2 split. Bipartite's 1:1 rule
	// lets greta's finding corroborate only ONE kai finding; the other stands
	// alone.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "alpha beta gamma delta epsilon zeta eta", "greta"), // A
		fnd("a.go", 1, "alpha beta gamma delta epsilon zeta p1", "kai"),    // P: vs A 0.75 merge
		fnd("a.go", 1, "alpha beta gamma delta epsilon zeta q1", "kai"),    // Q: vs A 0.75 merge
	})
	// Greedy would have produced [3]; bipartite produces a corroborated pair plus
	// the uncorroborated extra.
	deepEq(t, groupSizes(groups), []int{1, 2}, "consensus pair + one stranded finding (not a 3-way over-merge)")
	length(t, amb, 0, "P/Q are same-source and never compared, so no gray pair is recorded")
}

func TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates(t *testing.T) {
	// A~B is only gray (0.667 — not duplicates). C clears the merge threshold
	// against both (0.818). Greedy chains A-B-C into one group of 3, silently
	// merging the non-duplicate A-B. Bipartite keeps A and B apart (C corroborates
	// one of them) and surfaces the unresolved A-B pair as ambiguous.
	groups, amb := DedupeCluster([]Finding{
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 w10", "greta"), // A
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 c1 b1", "kai"),    // B: vs A 0.667 gray
		fnd("a.go", 1, "w1 w2 w3 w4 w5 w6 w7 w8 w9 c1", "mira"),   // C: vs A,B 0.818 merge
	})
	deepEq(t, groupSizes(groups), []int{1, 2}, "non-duplicate A-B not chained via C")
	length(t, amb, 1, "the unresolved gray A-B pair is recorded for adjudication")
}

func TestAC2_CompleteLinkageRejectsMergeStrengthChain(t *testing.T) {
	// Two MERGE-strength links (a~b and b~c each >= 0.7) with NON-duplicate
	// endpoints (a~c below the merge threshold). Single-linkage acceptance
	// (mergeable for ANY current member) lets c join {a,b} via b and drags the
	// non-duplicate a and c into one group. Complete-linkage acceptance (mergeable
	// for ALL members) keeps a and c apart. This differs from
	// TestAC2_TransitiveBridgeDoesNotMergeNonDuplicates, whose bridge is a single
	// GRAY link — here BOTH bridge links clear the merge threshold.
	a := fnd("a.go", 1, "t1 t2 t3 t4 t5 t6 t7 t8", "ra")  // a
	b := fnd("a.go", 1, "t1 t2 t3 t4 t5 t6", "rb")        // b: a~b inter6/union8 = 0.75 merge
	c := fnd("a.go", 1, "t1 t2 t3 t4 t5 t6 t9 t10", "rc") // c: b~c 0.75 merge; a~c inter6/union10 = 0.6 (not merge)
	groups, _ := DedupeCluster([]Finding{a, b, c})
	groupOfA, groupOfC := -1, -2
	for gi, g := range groups {
		for _, f := range g {
			if f.Reviewer == "ra" {
				groupOfA = gi
			}
			if f.Reviewer == "rc" {
				groupOfC = gi
			}
		}
	}
	notEq(t, groupOfA, groupOfC, "non-duplicate endpoints a and c stay in separate groups (no single-linkage chain)")
}

func TestAC2_OptimalAssignmentPrefersClosestPairing(t *testing.T) {
	// Both reviewers report two findings at one location, with cross-overlap so a
	// naive matcher could pair them wrong. greta:[A,B], kai:[P,Q] where A~Q and
	// B~P are the true duplicates (>=0.7) while A~P and B~Q are only gray. Optimal
	// assignment recovers the two correct pairs; it must not strand a finding or
	// pair on the weaker edge.
	groups, _ := DedupeCluster([]Finding{
		fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 aone", "greta"), // A
		fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 bone", "greta"), // B
		fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 bone", "kai"),   // P == B text → B~P merge
		fnd("a.go", 1, "shared1 shared2 shared3 shared4 shared5 shared6 shared7 aone", "kai"),   // Q == A text → A~Q merge
	})
	deepEq(t, groupSizes(groups), []int{2, 2}, "two correct cross-source pairs, no stranded finding")
	// Verify the pairing is by identical text (A with Q, B with P), not crossed.
	for _, g := range groups {
		length(t, g, 2, "each group is a clean cross-source pair")
		eq(t, g[0].Problem == g[1].Problem, true, "paired findings share the same problem text")
	}
}

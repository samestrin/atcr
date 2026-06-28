package reconcile

import "testing"

// The composite edge-weight distance (Epic 13.2): 0 when two findings share a
// non-empty AST GroupKey (13.1 isomorphism), else 1 - token-set Jaccard.

func TestPairDistance_SameASTKeyIsZero(t *testing.T) {
	// Identical non-empty key dominates even when the problem texts are disjoint.
	d := pairDistance("a.go\x00H", "a.go\x00H", tokenize("alpha beta"), tokenize("gamma delta"))
	eq(t, d, 0.0, "shared non-empty AST key → distance 0")
}

func TestPairDistance_DifferentASTKeysFallToJaccard(t *testing.T) {
	// Different keys → composite ignores the key and uses 1 - Jaccard. Disjoint
	// texts → Jaccard 0 → distance 1.
	d := pairDistance("a.go\x00H", "a.go\x00K", tokenize("alpha beta"), tokenize("gamma delta"))
	eq(t, d, 1.0, "different keys, disjoint text → distance 1")
}

func TestPairDistance_EmptyKeysUseJaccard(t *testing.T) {
	// Proximity-grouped (empty key) findings rank by 1 - Jaccard.
	// {alpha,beta,gamma} vs {alpha,beta,delta}: 2 shared / 4 union = 0.5 → 0.5.
	d := pairDistance("", "", tokenize("alpha beta gamma"), tokenize("alpha beta delta"))
	inDelta(t, d, 0.5, 1e-9, "empty keys → 1 - Jaccard")
}

func TestPairDistance_IdenticalTextEmptyKeysIsZero(t *testing.T) {
	d := pairDistance("", "", tokenize("token never expires"), tokenize("token never expires"))
	eq(t, d, 0.0, "identical text → distance 0")
}

func TestPairDistance_OneEmptyKeyIsNotAMatch(t *testing.T) {
	// One keyed, one unkeyed: not a shared key, so fall to Jaccard.
	d := pairDistance("a.go\x00H", "", tokenize("alpha beta"), tokenize("alpha beta"))
	eq(t, d, 0.0, "identical text still 0 via Jaccard")
	d2 := pairDistance("a.go\x00H", "", tokenize("alpha beta"), tokenize("gamma delta"))
	eq(t, d2, 1.0, "disjoint text via Jaccard despite one key present")
}

func TestPairDistance_Symmetric(t *testing.T) {
	tokA, tokB := tokenize("alpha beta gamma"), tokenize("alpha beta delta")
	d1 := pairDistance("", "", tokA, tokB)
	d2 := pairDistance("", "", tokB, tokA)
	eq(t, d1, d2, "distance is symmetric in its arguments")
}

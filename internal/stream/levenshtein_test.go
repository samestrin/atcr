package stream

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLevenshtein_Distance covers the canonical edit-distance cases the Tier 2
// typo matcher depends on, including the motivating 5.0 example.
//
// NOTE: the 5.0 plan (and this epic's Open Question 1) asserted that
// validator->validate is edit distance 1; it is actually 2 (shared prefix
// "validat", then "or" vs "e" = one substitution + one deletion). The Tier 2
// threshold is tuned to the real example, not the mis-stated number — see
// tier2SimilarityThreshold in suggest.go.
func TestLevenshtein_Distance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"validator", "validate", 2},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
		{"Parser", "parser", 1}, // single case-differing char
	}
	for _, c := range cases {
		got := levenshtein(c.a, c.b)
		assert.Equalf(t, c.want, got, "levenshtein(%q,%q)", c.a, c.b)
	}
}

// TestSimilarity returns a 0..1 ratio normalized by the longer string; identical
// strings are 1.0, fully disjoint approaches 0. The validator/validate stem pair
// clears the tuned Tier 2 0.75 threshold; config/cfg does not.
func TestSimilarity(t *testing.T) {
	assert.Equal(t, 1.0, similarity("", ""))
	assert.Equal(t, 1.0, similarity("abc", "abc"))
	assert.InDelta(t, 0.777, similarity("validator", "validate"), 0.01)
	assert.True(t, similarity("validator", "validate") >= 0.75)
	assert.True(t, similarity("config", "cfg") < 0.75)
}

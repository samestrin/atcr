package stream

// levenshtein returns the edit distance (insertions, deletions, substitutions)
// between a and b. It backs the Tier 2 typo matcher, which compares a cited
// filename's stem against the real files in the same directory. The
// implementation uses the standard two-row dynamic-programming table, O(len(a)*
// len(b)) time and O(min) extra space.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	// Keep the inner loop over the shorter string to bound the row length.
	if len(ra) < len(rb) {
		ra, rb = rb, ra
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	curr := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		curr[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(rb)]
}

// similarity is a 0..1 closeness ratio normalized by the longer input: 1.0 for
// identical strings, approaching 0 as they diverge. Two empty strings are
// defined as identical (1.0). Tier 2 compares stems against
// tier2SimilarityThreshold using this metric.
func similarity(a, b string) float64 {
	if a == b {
		return 1.0
	}
	la, lb := len([]rune(a)), len([]rune(b))
	longest := la
	if lb > longest {
		longest = lb
	}
	if longest == 0 {
		return 1.0
	}
	return 1.0 - float64(levenshtein(a, b))/float64(longest)
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

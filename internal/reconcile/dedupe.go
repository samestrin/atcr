package reconcile

import (
	"regexp"
	"strings"

	"github.com/samestrin/atcr/internal/stream"
)

// Fixed v1 similarity thresholds (not configurable, per AC 01-05). Findings in a
// location cluster whose normalized PROBLEM texts have token-set Jaccard
// similarity at or above MergeThreshold are duplicates; below GrayLow they are
// distinct; in the gray zone [GrayLow, MergeThreshold) they are ambiguous —
// recorded for adjudication and left unmerged (conservative default).
const (
	MergeThreshold = 0.7
	GrayLow        = 0.4
)

// tokenSplit splits normalized text on any run of non-alphanumeric characters.
var tokenSplit = regexp.MustCompile(`[^a-z0-9]+`)

// AmbiguousCluster is a same-location pair whose PROBLEM similarity fell in the
// gray zone. It is serialized to ambiguous.json so the Skill (or a human) can
// adjudicate; the two findings remain unmerged in the reconciled output. Line is
// the lower-indexed finding's line (the pair may span the ±3 window); each
// finding's own line is in Findings.
type AmbiguousCluster struct {
	File       string           `json:"file"`
	Line       int              `json:"line"`
	Similarity float64          `json:"similarity"`
	Findings   []stream.Finding `json:"findings"`
}

// DedupeCluster partitions one location cluster into merge groups and records
// ambiguous pairs. Findings linked by similarity >= MergeThreshold (single-
// linkage, transitively) form a merge group to be collapsed downstream;
// everything else stays in its own singleton group. Every pair scoring in
// [GrayLow, MergeThreshold) is recorded as an AmbiguousCluster but is NOT merged.
// Group order follows first appearance for determinism. Token sets are computed
// once per finding (not per pair) so the O(n^2) pair loop stays cheap.
func DedupeCluster(cluster []stream.Finding) ([][]stream.Finding, []AmbiguousCluster) {
	n := len(cluster)
	tokens := make([]map[string]struct{}, n)
	for i, f := range cluster {
		tokens[i] = tokenize(f.Problem)
	}

	uf := newUnionFind(n)
	var ambiguous []AmbiguousCluster
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			rel, sim := classify(tokens[i], tokens[j])
			switch rel {
			case relMerge:
				uf.union(i, j)
			case relGray:
				ambiguous = append(ambiguous, AmbiguousCluster{
					File:       cluster[i].File,
					Line:       cluster[i].Line,
					Similarity: sim,
					Findings:   []stream.Finding{cluster[i], cluster[j]},
				})
			}
		}
	}

	groupMap := map[int][]stream.Finding{}
	var roots []int
	for i := 0; i < n; i++ {
		r := uf.find(i)
		if _, seen := groupMap[r]; !seen {
			roots = append(roots, r)
		}
		groupMap[r] = append(groupMap[r], cluster[i])
	}
	groups := make([][]stream.Finding, 0, len(roots))
	for _, r := range roots {
		groups = append(groups, groupMap[r])
	}
	return groups, ambiguous
}

// relation is the dedupe outcome for one pair.
type relation int

const (
	relDistinct relation = iota
	relGray
	relMerge
)

// classify decides merge/gray/distinct from two token sets, returning the
// display similarity alongside. The threshold comparisons use integer
// cross-multiplication (inter*10 vs union*N) so the fixed 0.7/0.4 boundaries are
// exact and never depend on float rounding — critical for a deterministic CI
// gate. Two empty problem texts are identical (merge); one empty side is
// distinct (nothing to compare).
func classify(a, b map[string]struct{}) (relation, float64) {
	if len(a) == 0 && len(b) == 0 {
		return relMerge, 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return relDistinct, 0.0
	}
	inter := 0
	for t := range a {
		if _, ok := b[t]; ok {
			inter++
		}
	}
	union := len(a) + len(b) - inter
	sim := float64(inter) / float64(union)
	switch {
	case inter*10 >= union*7: // >= 0.7
		return relMerge, sim
	case inter*10 >= union*4: // >= 0.4
		return relGray, sim
	default:
		return relDistinct, sim
	}
}

// jaccard returns the token-set Jaccard similarity of a and b in [0,1]. Empty
// input on either side yields 0. (Decisions use classify; jaccard is retained
// for direct similarity inspection and tests.)
func jaccard(a, b string) float64 {
	ta, tb := tokenize(a), tokenize(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	inter := 0
	for t := range ta {
		if _, ok := tb[t]; ok {
			inter++
		}
	}
	union := len(ta) + len(tb) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// tokenize lowercases s and returns its set of alphanumeric word tokens.
func tokenize(s string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, tok := range tokenSplit.Split(strings.ToLower(s), -1) {
		if tok != "" {
			set[tok] = struct{}{}
		}
	}
	return set
}

// unionFind is a tiny disjoint-set for single-linkage grouping within a cluster.
type unionFind struct{ parent []int }

func newUnionFind(n int) *unionFind {
	p := make([]int, n)
	for i := range p {
		p[i] = i
	}
	return &unionFind{parent: p}
}

func (u *unionFind) find(x int) int {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]] // path halving
		x = u.parent[x]
	}
	return x
}

func (u *unionFind) union(a, b int) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	// Union toward the smaller root so component representatives are stable.
	if ra < rb {
		u.parent[rb] = ra
	} else {
		u.parent[ra] = rb
	}
}

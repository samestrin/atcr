package reconcile

import (
	"bytes"
	"strings"
)

// Fixed v1 NCD thresholds (not configurable). Findings in a location cluster
// whose normalized text has a Normalized Compression Distance at or below
// MergeMaxNCD are duplicates; above GrayMaxNCD they are distinct; in the gray
// zone (MergeMaxNCD, GrayMaxNCD] they are ambiguous — recorded for adjudication
// and left unmerged (conservative default).
//
// NCD is a DISTANCE (0 = identical, ~1 = unrelated), so these comparisons are
// inverted from the Jaccard token-set similarity they replaced. The cutoffs are
// expressed as integer per-mille values because the gate in classify compares
// them by integer cross-multiplication (num*1000 vs cutoff*max) — exact and
// deterministic, never dependent on float rounding, exactly as the prior Jaccard
// cross-multiply was. Calibrated against testdata/ncd_corpus (see
// ncd_corpus_test.go).
const (
	ncdMergeMaxMille = 550 // NCD <= 0.550 → duplicates (merge)
	ncdGrayMaxMille  = 750 // 0.550 < NCD <= 0.750 → ambiguous (gray); above → distinct
)

// MergeThreshold and GrayLow are the advisory similarity-space (1 - NCD)
// equivalents of the authoritative integer cutoffs above, retained as part of the
// package's public surface. They are display/reference values only — the gate in
// classify never consults them, so they cannot reintroduce float rounding into
// the deterministic decision.
const (
	MergeThreshold = 0.450 // 1 - 0.550
	GrayLow        = 0.250 // 1 - 0.750
)

// AmbiguousCluster is a same-location pair whose NCD fell in the gray zone. It is
// serialized to the ambiguous sidecar so a host (or a human) can adjudicate; the
// two findings remain unmerged in the reconciled output. ID is the stable
// content-addressed handle referenced across runs. Line is the lower-indexed
// finding's line (the pair may span the ±3 window); each finding's own line is in
// Findings. Similarity is the advisory display score (1 - NCD): ~1 for
// near-identical, ~0 for unrelated.
type AmbiguousCluster struct {
	ID         string    `json:"id"`
	File       string    `json:"file"`
	Line       int       `json:"line"`
	Similarity float64   `json:"similarity"`
	Findings   []Finding `json:"findings"`
}

// DedupeCluster partitions one location cluster into merge groups and records
// ambiguous pairs, with no adjudication applied. See dedupeCluster.
func DedupeCluster(cluster []Finding) ([][]Finding, []AmbiguousCluster) {
	return dedupeCluster(cluster, nil)
}

// dedupeCluster partitions one location cluster into merge groups and records
// ambiguous pairs. Findings linked by NCD <= MergeMaxNCD (single-linkage,
// transitively) form a merge group to be collapsed downstream; everything else
// stays in its own singleton group. Every pair scoring in the gray zone is
// recorded as an AmbiguousCluster but is NOT merged — UNLESS its content id is
// present in adjudicatedMerges, in which case the pair is unioned (merged) and
// not re-recorded as ambiguous. Group order follows first appearance for
// determinism. Each finding's normalized text is compressed once (not per pair)
// so the O(n^2) pair loop only pays for the concatenation compression.
func dedupeCluster(cluster []Finding, adjudicatedMerges map[string]bool) ([][]Finding, []AmbiguousCluster) {
	n := len(cluster)
	enc := ncdEncoder()
	scratch := make([]byte, 0, 4096) // reused compression destination
	texts := make([][]byte, n)
	csz := make([]int, n)
	for i, f := range cluster {
		texts[i] = ncdText(f)
		csz[i] = csize(enc, texts[i], scratch)
	}
	cat := make([]byte, 0, 8192) // reused concatenation buffer

	uf := newUnionFind(n)
	var ambiguous []AmbiguousCluster
	var ambiguousPairs [][2]int // index pair behind each ambiguous entry, for the post-loop root check
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			var rel relation
			var sim float64
			if bytes.Equal(texts[i], texts[j]) {
				// Byte-identical normalized text is unconditionally a duplicate,
				// independent of the compressor — this also covers two empty
				// findings (both merge) deterministically.
				rel, sim = relMerge, 1.0
			} else {
				// concatSize is order-independent (min of both concatenation
				// orders), so the advisory Similarity is a pure function of the
				// two findings, not their position in the cluster.
				cab := concatSize(enc, texts[i], texts[j], cat, scratch)
				rel, sim = classify(cab, csz[i], csz[j])
			}
			switch rel {
			case relMerge:
				uf.union(i, j)
			case relGray:
				id := AmbiguousID(cluster[i].File, cluster[i].Line, cluster[i].Problem, cluster[j].Problem)
				if adjudicatedMerges[id] {
					uf.union(i, j) // adjudicated this pair a duplicate
					continue
				}
				ambiguous = append(ambiguous, AmbiguousCluster{
					ID:         id,
					File:       cluster[i].File,
					Line:       cluster[i].Line,
					Similarity: sim,
					Findings:   []Finding{cluster[i], cluster[j]},
				})
				ambiguousPairs = append(ambiguousPairs, [2]int{i, j})
			}
		}
	}

	// A gray pair whose two findings ended up under the same union-find root
	// (transitively merged via a third finding) is no longer adjudicable: a
	// "merge" decision would be a silent no-op and "distinct" cannot be honored
	// since the pair is already collapsed. Drop those entries so the ambiguous
	// sidecar never contradicts the merged output.
	kept := ambiguous[:0]
	for k, p := range ambiguousPairs {
		if uf.find(p[0]) != uf.find(p[1]) {
			kept = append(kept, ambiguous[k])
		}
	}
	ambiguous = kept

	groupMap := map[int][]Finding{}
	var roots []int
	for i := 0; i < n; i++ {
		r := uf.find(i)
		if _, seen := groupMap[r]; !seen {
			roots = append(roots, r)
		}
		groupMap[r] = append(groupMap[r], cluster[i])
	}
	groups := make([][]Finding, 0, len(roots))
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

// classify decides merge/gray/distinct from the three compressed sizes of a pair
// (cab = C(concat), ca = C(a), cb = C(b)), returning the advisory display
// similarity (1 - NCD) alongside.
//
// The threshold comparisons use integer cross-multiplication (num*1000 vs
// cutoff*max) so the fixed 0.55/0.75 NCD boundaries are exact and never depend on
// float rounding — critical for a deterministic gate. The returned similarity is
// advisory only (display/tests) and MUST NOT drive the decision: a float NCD
// comparison would reintroduce the rounding nondeterminism the deterministic gate
// forbids.
//
// num = max(0, cab - min(ca,cb)) and max = max(ca,cb), mirroring the NCD numerator
// and denominator. max == 0 is unreachable for real input (zstd always emits a
// frame header) but is treated as identical (merge) defensively.
func classify(cab, ca, cb int) (relation, float64) {
	lo, hi := ca, cb
	if hi < lo {
		lo, hi = hi, lo
	}
	num := cab - lo
	if num < 0 {
		num = 0
	}
	if hi == 0 {
		return relMerge, 1.0
	}
	sim := 1.0 - float64(num)/float64(hi)
	if sim < 0 {
		sim = 0
	}
	if sim > 1 {
		sim = 1
	}
	switch {
	case num*1000 <= ncdMergeMaxMille*hi: // NCD <= 0.550
		return relMerge, sim
	case num*1000 <= ncdGrayMaxMille*hi: // NCD <= 0.750
		return relGray, sim
	default:
		return relDistinct, sim
	}
}

// ncdText returns the normalized bytes used for NCD scoring: the finding's
// Problem, Fix, and Evidence lowercased and newline-joined. Concatenating all
// three text columns gives the compressor enough material (typically 50-150 words
// for a real finding) to expose the cross-finding redundancy that a one-line
// Problem alone is too short to reveal — the regime where NCD reliably separates
// duplicates from distinct findings.
func ncdText(f Finding) []byte {
	var b strings.Builder
	b.WriteString(strings.ToLower(f.Problem))
	b.WriteByte('\n')
	b.WriteString(strings.ToLower(f.Fix))
	b.WriteByte('\n')
	b.WriteString(strings.ToLower(f.Evidence))
	return []byte(b.String())
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

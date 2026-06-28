package reconcile

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Fixed v1 similarity thresholds (not configurable). Findings in a location
// cluster whose normalized PROBLEM texts have token-set Jaccard similarity at or
// above MergeThreshold are duplicates; below GrayLow they are distinct; in the
// gray zone [GrayLow, MergeThreshold) they are ambiguous — recorded for
// adjudication and left unmerged (conservative default).
const (
	MergeThreshold = 0.7
	GrayLow        = 0.4
)

// maxClusterSize bounds the findings in one location cluster that dedupeCluster
// will fully process. Beyond it the cluster is adversarial — real clusters are a
// handful of findings — so the O(n^2) distance matrix and the pairwise
// matching/DBSCAN/gray passes are skipped to avoid a memory/CPU DoS. It matches
// the Hungarian solver's own size limit (see hungarian in bipartite.go) so the
// matching pass is never asked to exceed its bound.
const maxClusterSize = 500

// tokenSplit splits normalized text on any run of non-alphanumeric characters.
var tokenSplit = regexp.MustCompile(`[^a-z0-9]+`)

// AmbiguousCluster is a same-location pair whose PROBLEM similarity fell in the
// gray zone. It is serialized to the ambiguous sidecar so a host (or a human) can
// adjudicate; the two findings remain unmerged in the reconciled output. ID is
// the stable content-addressed handle referenced across runs. Line is the
// lower-indexed finding's line (the pair may span the ±3 window); each finding's
// own line is in Findings.
type AmbiguousCluster struct {
	ID         string    `json:"id"`
	File       string    `json:"file"`
	Line       int       `json:"line"`
	Similarity float64   `json:"similarity"`
	Findings   []Finding `json:"findings"`
}

// DedupeCluster partitions one location cluster into merge groups and records
// ambiguous pairs, with no adjudication and no AST grouping applied. See
// dedupeCluster.
func DedupeCluster(cluster []Finding) ([][]Finding, []AmbiguousCluster) {
	return dedupeCluster(cluster, make([]string, len(cluster)), nil)
}

// dedupeCluster partitions one location cluster into merge groups and records
// ambiguous pairs. Merge decisions come from optimal bipartite matching
// (bipartiteGroups): findings from different sources are matched 1:1 by the
// composite edge-weight distance (AST GroupKey from keys[i], else 1 - Jaccard),
// so each consensus issue gathers at most one finding per source and the greedy
// single-linkage over-merge is structurally avoided. keys[i] is finding i's AST
// group key (empty when none); it must be the same length as cluster.
//
// Every cross-group pair whose PROBLEM similarity falls in [GrayLow,
// MergeThreshold) is recorded as an AmbiguousCluster but left unmerged — UNLESS
// its content id is in adjudicatedMerges, in which case the two findings' groups
// are unioned (merged) and the pair is not re-recorded. A gray pair whose groups
// end up merged transitively (via adjudication of another pair) is dropped, so
// the sidecar never contradicts the merged output. Token sets are computed once
// per finding so the O(n^2) gray-pair scan stays cheap.
func dedupeCluster(cluster []Finding, keys []string, adjudicatedMerges map[string]bool) ([][]Finding, []AmbiguousCluster) {
	n := len(cluster)
	if n == 0 {
		return nil, nil
	}
	if len(keys) != n {
		panic(fmt.Sprintf("dedupeCluster: len(keys)=%d must equal len(cluster)=%d", len(keys), n))
	}
	if n > maxClusterSize {
		// DoS safety valve: a location cluster this large is adversarial (real
		// clusters are a handful of findings). Skip the O(n^2) distance matrix and
		// the pairwise matching/DBSCAN/gray passes entirely; pass every finding
		// through as its own singleton group — bounded O(n) work, no dedup, no
		// ambiguous/noise sidecar. Bounding here also keeps every downstream
		// Hungarian problem within its size limit.
		out := make([][]Finding, n)
		for i := range cluster {
			out[i] = []Finding{cluster[i]}
		}
		return out, nil
	}
	tokens := make([]map[string]struct{}, n)
	for i, f := range cluster {
		tokens[i] = tokenize(f.Problem)
	}
	// Precompute the relation+similarity matrix ONCE per cluster. classify is the
	// hot path — the distance-matrix build, the mergeable predicate, and the
	// gray-pair scan each need a pair's relation, so without caching the same pair
	// is classified up to three times. The integer-exact relation is stored
	// verbatim and every site below reads rel[i][j] (never a fresh float
	// comparison), so the deterministic 0.7/0.4 boundary is preserved. The
	// composite distance is derived inline (mirroring pairDistance): 0 when the
	// pair shares a non-empty AST key (the 13.1 isomorphism signal), else 1-Jaccard.
	rel := make([][]relation, n)
	sim := make([][]float64, n)
	dist := make([][]float64, n)
	for i := range rel {
		rel[i] = make([]relation, n)
		sim[i] = make([]float64, n)
		dist[i] = make([]float64, n)
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			r, s := classify(tokens[i], tokens[j])
			rel[i][j], rel[j][i] = r, r
			sim[i][j], sim[j][i] = s, s
			d := distanceCeiling - s
			if keys[i] != "" && keys[i] == keys[j] {
				d = distanceFloor
			}
			dist[i][j], dist[j][i] = d, d
		}
	}

	// srcKeys identifies each finding's source for matching. A non-empty Reviewer
	// is the source; an empty Reviewer (unattributed finding) gets a per-finding
	// unique key so it is treated as its own source — otherwise every unattributed
	// finding would collapse into one pseudo-source and nothing would ever match,
	// silently disabling dedup. The real-reviewer and anon key spaces carry
	// DISTINCT NUL-delimited prefixes so an attacker-controlled Reviewer string —
	// which may itself contain NUL — can never forge a finding's anon key and be
	// mis-treated as that finding's source: a crafted Reviewer "\x00anon\x000" maps
	// to "\x00src\x00\x00anon\x000", which can never equal an "\x00anon\x00<i>" key.
	srcKeys := make([]string, n)
	for i, f := range cluster {
		if f.Reviewer != "" {
			srcKeys[i] = "\x00src\x00" + f.Reviewer
		} else {
			srcKeys[i] = "\x00anon\x00" + strconv.Itoa(i)
		}
	}

	// mergeable is the integer-exact duplicate predicate: a shared non-empty AST
	// key, or token-set Jaccard at/above MergeThreshold (classify's cross-multiply
	// boundary, never the float distance). It gates bipartite acceptance so the
	// merge decision stays deterministic.
	mergeable := func(a, b int) bool {
		if keys[a] != "" && keys[a] == keys[b] {
			return true
		}
		return rel[a][b] == relMerge
	}

	groups := bipartiteGroups(srcKeys, dist, mergeable)
	groupOf := make([]int, n)
	for gi, g := range groups {
		for _, idx := range g {
			groupOf[idx] = gi
		}
	}

	// Adjudicated gray pairs force-merge groups; union-find runs over GROUP ids.
	uf := newUnionFind(len(groups))
	var ambiguous []AmbiguousCluster
	var ambiguousPairs [][2]int // group-id pair behind each entry, for the post-loop root check
	var ambiguousIdx [][2]int   // finding-index pair behind each entry, for the noise exclusion
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if groupOf[i] == groupOf[j] {
				continue // matched into one group already; not adjudicable
			}
			// Same non-empty AST key is authoritative duplicate evidence: do not
			// record the pair as gray even if token Jaccard falls in the gray band.
			if keys[i] != "" && keys[i] == keys[j] {
				continue
			}
			if rel[i][j] != relGray {
				continue
			}
			id := AmbiguousID(cluster[i].File, cluster[i].Line, cluster[i].Problem, cluster[j].Problem)
			if adjudicatedMerges[id] {
				uf.union(groupOf[i], groupOf[j]) // adjudicated this pair a duplicate
				continue
			}
			ambiguous = append(ambiguous, AmbiguousCluster{
				ID:         id,
				File:       cluster[i].File,
				Line:       cluster[i].Line,
				Similarity: sim[i][j],
				Findings:   []Finding{cluster[i], cluster[j]},
			})
			ambiguousPairs = append(ambiguousPairs, [2]int{groupOf[i], groupOf[j]})
			ambiguousIdx = append(ambiguousIdx, [2]int{i, j})
		}
	}

	// Drop gray pairs whose groups were transitively merged by adjudication: a
	// "merge" decision would be a silent no-op and "distinct" can no longer be
	// honored, so the entry must not contradict the merged output.
	keptAmb := ambiguous[:0]
	keptIdx := ambiguousIdx[:0]
	for k, gp := range ambiguousPairs {
		if uf.find(gp[0]) != uf.find(gp[1]) {
			keptAmb = append(keptAmb, ambiguous[k])
			keptIdx = append(keptIdx, ambiguousIdx[k])
		}
	}
	ambiguous = keptAmb

	// A finding already surfaced in a surviving gray pair must not also be isolated
	// as DBSCAN noise (no double representation in the sidecar).
	grayInvolved := make(map[int]bool, len(keptIdx)*2)
	for _, p := range keptIdx {
		grayInvolved[p[0]] = true
		grayInvolved[p[1]] = true
	}

	// Materialize merge groups, folding adjudicated group unions together. Root
	// order follows group order (already normalized by lowest member index), so the
	// merge representative stays the lowest-index finding.
	rootIdx := map[int][]int{}
	var rootOrder []int
	for gi, g := range groups {
		r := uf.find(gi)
		if _, seen := rootIdx[r]; !seen {
			rootOrder = append(rootOrder, r)
		}
		rootIdx[r] = append(rootIdx[r], g...)
	}
	soloRoot := make(map[int]bool, n) // finding index → it is the only member of its root
	for _, idxs := range rootIdx {
		if len(idxs) == 1 {
			soloRoot[idxs[0]] = true
		}
	}

	// DBSCAN over the same location cluster isolates uncorroborated noise. The
	// eps-neighborhood is a CROSS-SOURCE merge: density must come from two distinct
	// sources agreeing (real corroboration), not one source repeating itself — a
	// self-duplicate is not evidence that a different source's finding is noise.
	// Noise is isolated ONLY when such a dense cluster also exists in this location
	// (a finding standing alone where others corroborate each other is a likely
	// single-model hallucination; a finding whose location no one else touched is
	// just an uncorroborated report and stays in the output), and only when the
	// finding is genuinely alone — not gray-paired and not merged into any group —
	// so it is never represented in both the output and the sidecar.
	// denseSrc is the DENSITY source key, distinct from srcKeys: it collapses ALL
	// unattributed findings into one shared "" source so two empty-Reviewer copies
	// of the same finding are NOT independent corroboration (srcKeys keeps each its
	// own per-finding key for bipartite matching, which density must not reuse —
	// every unattributed finding would otherwise count as a distinct corroborating
	// source). A named reviewer remains its own source, so a named finding still
	// corroborates a different source (named or unattributed).
	denseSrc := make([]string, n)
	for i, f := range cluster {
		denseSrc[i] = f.Reviewer
	}
	denseNeighbor := func(a, b int) bool { return mergeable(a, b) && denseSrc[a] != denseSrc[b] }
	labels, dense := dbscanLabels(n, dbscanMinPts, denseNeighbor)
	noiseIdx := map[int]bool{}
	var noise []AmbiguousCluster
	if dense {
		for i := 0; i < n; i++ {
			if labels[i] != dbscanNoise || grayInvolved[i] || !soloRoot[i] {
				continue
			}
			noiseIdx[i] = true
			noise = append(noise, AmbiguousCluster{
				// A single-finding cluster: same-problem id keeps it stable and
				// distinct from any two-problem gray-pair id; Similarity stays 0
				// (no corroboration). debate skips clusters with < 2 findings.
				ID:       AmbiguousID(cluster[i].File, cluster[i].Line, cluster[i].Problem, cluster[i].Problem),
				File:     cluster[i].File,
				Line:     cluster[i].Line,
				Findings: []Finding{cluster[i]},
			})
		}
	}

	out := make([][]Finding, 0, len(rootOrder))
	for _, r := range rootOrder {
		idxs := rootIdx[r]
		if len(idxs) == 1 && noiseIdx[idxs[0]] {
			continue // isolated as DBSCAN noise → sidecar only
		}
		sort.Ints(idxs) // stable member order after any adjudicated union
		members := make([]Finding, len(idxs))
		for k, idx := range idxs {
			members[k] = cluster[idx]
		}
		out = append(out, members)
	}
	ambiguous = append(ambiguous, noise...)
	return out, ambiguous
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
// exact and never depend on float rounding — critical for a deterministic gate.
// Two empty problem texts are identical (merge); one empty side is distinct
// (nothing to compare).
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
	// The integer cross-multiply below is the AUTHORITATIVE threshold boundary —
	// it is exact and deterministic. `sim` is advisory only (display/tests) and
	// MUST NOT replace the cross-multiply: a `sim >= 0.7` float comparison would
	// reintroduce rounding nondeterminism the deterministic gate forbids.
	//
	// `sim` is serialized into AmbiguousCluster.Similarity and hashed via
	// AmbiguousHash. That byte-identity holds because IEEE-754 division of the
	// same two integers produces the same bits on every supported Go platform,
	// and encoding/json emits those bits deterministically. A future schema
	// version may replace the float with an integer ratio, but that is a
	// breaking change that retires the current golden fixtures.
	switch {
	case inter*10 >= union*7: // >= 0.7
		return relMerge, sim
	case inter*10 >= union*4: // >= 0.4
		return relGray, sim
	default:
		return relDistinct, sim
	}
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

// unionFind is a tiny disjoint-set used to fold adjudicated gray-pair merges over
// the bipartite group ids within a cluster.
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

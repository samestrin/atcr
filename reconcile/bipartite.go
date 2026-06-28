package reconcile

import (
	"fmt"
	"math"
	"sort"
)

// Bipartite matching for Epic 13.2. The greedy union-find dedup (single-linkage
// over a similarity threshold) is replaced by greedy incremental pairwise
// matching: within a location cluster, findings from different sources are
// matched 1:1 by the Kuhn-Munkres (Hungarian) algorithm using the composite
// edge-weight distance (see distance.go). Each pairwise step is optimal, but the
// N-way result depends on the order in which sources are processed (bounded for
// small N). The 1:1 constraint is the structural fix for the greedy failure mode
// — single-linkage transitively chains A-B-C even when A and B are only weakly
// similar; matching pairs each finding with at most one counterpart per other
// source, so a third finding cannot drag two non-duplicates into one group.

// noMatchSentinel is the cost charged to padded (virtual) cells when squaring a
// rectangular cost matrix. It exceeds any real composite distance so the
// assignment never prefers a virtual cell over a feasible real one.
const noMatchSentinel = distanceCeiling + 1.0

// hungarian solves the minimum-cost assignment problem for a square cost matrix
// and returns assign where assign[r] is the column matched to row r (a
// permutation of 0..n-1). It is the classic O(n^3) Kuhn-Munkres with potentials
// (Jonker-Volgenant shortest-augmenting-path form). Strict-less-than comparisons
// make the column tie-break favor the lowest index, so equal-cost inputs yield a
// deterministic, platform-stable assignment.
func hungarian(cost [][]float64) []int {
	n := len(cost)
	if n == 0 {
		return nil
	}
	const maxHungarianN = 500
	if n > maxHungarianN {
		panic(fmt.Sprintf("hungarian: matrix size %d exceeds maximum %d", n, maxHungarianN))
	}
	const inf = math.MaxFloat64
	// 1-indexed potentials/state per the canonical formulation; p[j] is the row
	// (1-indexed) assigned to column j, p[0] is the scratch slot.
	u := make([]float64, n+1)
	v := make([]float64, n+1)
	p := make([]int, n+1)
	way := make([]int, n+1)
	for i := 1; i <= n; i++ {
		p[0] = i
		j0 := 0
		minv := make([]float64, n+1)
		used := make([]bool, n+1)
		for j := 0; j <= n; j++ {
			minv[j] = inf
		}
		for iter := 0; ; iter++ {
			if iter > n {
				panic("hungarian: exceeded maximum shortest-augmenting-path iterations")
			}
			used[j0] = true
			i0 := p[j0]
			delta := inf
			j1 := -1
			for j := 1; j <= n; j++ {
				if used[j] {
					continue
				}
				cur := cost[i0-1][j-1] - u[i0] - v[j]
				if cur < minv[j] {
					minv[j] = cur
					way[j] = j0
				}
				if minv[j] < delta {
					delta = minv[j]
					j1 = j
				}
			}
			for j := 0; j <= n; j++ {
				if used[j] {
					u[p[j]] += delta
					v[j] -= delta
				} else {
					minv[j] -= delta
				}
			}
			j0 = j1
			if p[j0] == 0 {
				break
			}
		}
		for j0 != 0 {
			j1 := way[j0]
			p[j0] = p[j1]
			j0 = j1
		}
	}
	assign := make([]int, n)
	for j := 1; j <= n; j++ {
		if p[j] >= 1 {
			assign[p[j]-1] = j - 1
		}
	}
	return assign
}

// hungarianAssign runs the Hungarian algorithm over a rows×cols cost matrix
// (defined by cost(r,c)) and returns assign where assign[r] is the optimally
// matched real column, or -1 when row r was matched only to a virtual (padding)
// column because there were fewer real columns than rows. The matrix is padded to
// a square with noMatchSentinel so rectangular problems are well-formed and a real
// column is always preferred over padding. This is pure optimization: it does NOT
// apply a merge threshold — the caller decides acceptance with an exact predicate,
// so the float cost never gates a duplicate decision (see bipartiteGroups).
func hungarianAssign(rows, cols int, cost func(r, c int) float64) []int {
	assign := make([]int, rows)
	for r := range assign {
		assign[r] = -1
	}
	if rows == 0 || cols == 0 {
		return assign
	}
	n := rows
	if cols > n {
		n = cols
	}
	square := make([][]float64, n)
	for r := 0; r < n; r++ {
		square[r] = make([]float64, n)
		for c := 0; c < n; c++ {
			if r < rows && c < cols {
				square[r][c] = cost(r, c)
			} else {
				square[r][c] = noMatchSentinel
			}
		}
	}
	full := hungarian(square)
	for r := 0; r < rows; r++ {
		if c := full[r]; c < cols {
			assign[r] = c
		}
	}
	return assign
}

// bipartiteGroups partitions a location cluster into merge groups using the
// incremental pairwise reduction of N-way matching: findings are split by source
// (Reviewer), then each source's findings are greedily matched against the
// groups accumulated so far. Each pairwise step is optimal, but the overall
// N-way result is order-dependent because a candidate matched early locks a
// group that a later better-fitting candidate is then excluded from by the 1:1
// constraint. The group↔candidate cost is the single-linkage (minimum) composite
// distance to the group's members, so the assignment prefers pairing a candidate
// with the group it most resembles, while the 1:1 constraint forbids one
// candidate from collapsing two distinct groups (the structural fix for greedy
// single-linkage over-merge).
//
// Acceptance is gated by mergeable, NOT by the float cost: a candidate joins the
// group it was optimally assigned to only when mergeable(member, candidate) holds
// for some member — an integer-exact predicate (AST key match, or classify ==
// relMerge) that keeps the duplicate boundary deterministic and free of the
// floating-point rounding the cost matrix carries. An unaccepted or unmatched
// candidate seeds a new singleton group, so a group holds at most one finding per
// source — one consensus issue across reviewers.
//
// Sources are identified by srcKeys[i] (the finding's source-partition key, see
// dedupeCluster: a non-empty Reviewer, else a per-finding unique key so an
// unattributed finding is its own source and can still match others rather than
// collapsing every unattributed finding into one non-matchable pseudo-source).
//
// Determinism: sources are processed in sorted order; within a source, findings
// keep cluster order; the returned groups are normalized — members ascending,
// groups ordered by their lowest member index — so the merge representative
// (group[0]) is the lowest-index finding regardless of source processing order.
func bipartiteGroups(srcKeys []string, dist [][]float64, mergeable func(a, b int) bool) [][]int {
	bySource := map[string][]int{}
	var order []string
	for i, key := range srcKeys {
		if _, seen := bySource[key]; !seen {
			order = append(order, key)
		}
		bySource[key] = append(bySource[key], i)
	}
	sort.Strings(order)

	var groups [][]int
	for _, s := range order {
		cands := bySource[s]
		if len(groups) == 0 {
			for _, c := range cands {
				groups = append(groups, []int{c})
			}
			continue
		}
		assign := hungarianAssign(len(groups), len(cands), func(r, c int) float64 {
			m := distanceCeiling
			for _, gi := range groups[r] {
				if d := dist[gi][cands[c]]; d < m {
					m = d
				}
			}
			return m
		})
		used := make([]bool, len(cands))
		for r, c := range assign {
			if c < 0 {
				continue
			}
			cand := cands[c]
			accept := false
			for _, gi := range groups[r] {
				if mergeable(gi, cand) {
					accept = true
					break
				}
			}
			if accept {
				groups[r] = append(groups[r], cand)
				used[c] = true
			}
		}
		for ci, idx := range cands {
			if !used[ci] {
				groups = append(groups, []int{idx})
			}
		}
	}

	for _, g := range groups {
		sort.Ints(g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return groups[i][0] < groups[j][0]
	})
	return groups
}

package reconcile

import "testing"

// totalCost sums the chosen assignment's costs for validation.
func totalCost(cost [][]float64, assign []int) float64 {
	sum := 0.0
	for r, c := range assign {
		sum += cost[r][c]
	}
	return sum
}

// isPermutation verifies assign is a bijection rows→cols.
func isPermutation(assign []int, n int) bool {
	if len(assign) != n {
		return false
	}
	seen := make([]bool, n)
	for _, c := range assign {
		if c < 0 || c >= n || seen[c] {
			return false
		}
		seen[c] = true
	}
	return true
}

func TestHungarian_2x2Diagonal(t *testing.T) {
	cost := [][]float64{{1, 2}, {2, 1}}
	a := hungarian(cost)
	isTrue(t, isPermutation(a, 2), "valid permutation")
	eq(t, totalCost(cost, a), 2.0, "diagonal is optimal (1+1)")
}

func TestHungarian_2x2AntiDiagonal(t *testing.T) {
	cost := [][]float64{{2, 1}, {1, 2}}
	a := hungarian(cost)
	eq(t, totalCost(cost, a), 2.0, "anti-diagonal is optimal (1+1)")
	eq(t, a[0], 1, "row0→col1")
	eq(t, a[1], 0, "row1→col0")
}

func TestHungarian_3x3KnownOptimum(t *testing.T) {
	cost := [][]float64{{7, 5, 11}, {5, 4, 1}, {9, 3, 2}}
	a := hungarian(cost)
	isTrue(t, isPermutation(a, 3), "valid permutation")
	eq(t, totalCost(cost, a), 11.0, "min assignment cost is 11 (r0c0,r1c2,r2c1)")
}

func TestHungarian_Deterministic(t *testing.T) {
	// All-equal costs: every assignment is optimal; the algorithm must pick the
	// same one every time (lowest-column tie-break).
	cost := [][]float64{{0.5, 0.5, 0.5}, {0.5, 0.5, 0.5}, {0.5, 0.5, 0.5}}
	a1 := hungarian(cost)
	a2 := hungarian(cost)
	for i := range a1 {
		eq(t, a1[i], a2[i], "deterministic across runs")
	}
}

func TestHungarianAssign_Rectangular(t *testing.T) {
	// 1 row, 3 cols, all real; the row takes its cheapest column. No threshold
	// gate — the raw optimal assignment is returned.
	cost := func(r, c int) float64 {
		return []float64{0.25, 0.1, 0.2}[c]
	}
	assign := hungarianAssign(1, 3, cost)
	eq(t, assign[0], 1, "row0 takes cheapest col1 (0.1)")
}

func TestHungarianAssign_MoreRowsThanCols(t *testing.T) {
	// 3 rows, 1 col: only one row gets the real column; the cheapest wins, the
	// others are matched to padding → -1.
	cost := func(r, c int) float64 {
		return []float64{0.2, 0.05, 0.15}[r]
	}
	assign := hungarianAssign(3, 1, cost)
	eq(t, assign[1], 0, "cheapest row (row1, 0.05) takes the only col")
	eq(t, assign[0], -1, "row0 matched to padding")
	eq(t, assign[2], -1, "row2 matched to padding")
}

func TestHungarianAssign_RealColumnPreferredOverPadding(t *testing.T) {
	// 2 rows, 1 col: a high real cost (0.95) is still preferred over padding, so
	// one row keeps the real column (acceptance is the caller's job, not here).
	cost := func(r, c int) float64 {
		return []float64{0.95, 0.1}[r]
	}
	assign := hungarianAssign(2, 1, cost)
	eq(t, assign[1], 0, "cheaper row1 takes the real col")
	eq(t, assign[0], -1, "row0 to padding")
}

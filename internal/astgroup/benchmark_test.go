package astgroup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/require"
)

type benchCase struct {
	Name      string `json:"name"`
	File      string `json:"file"`
	Source    string `json:"source"`
	ALine     int    `json:"a_line"`
	BLine     int    `json:"b_line"`
	SameBlock bool   `json:"same_block"`
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestBenchmark_ASTGroupingAccuracy is the AC3 validation harness. It runs the
// AST grouper over a labeled corpus of finding pairs and compares it to the
// incumbent ±3 line-proximity rule on two axes:
//   - recall on same-block positives (the off-by-one / drift discrepancies AC3
//     must resolve): target ≥95%;
//   - precision (no false grouping of pairs in genuinely different blocks).
//
// It also asserts the AST signal strictly dominates proximity: it resolves every
// large-drift (>3) same-block pair proximity misses, and never makes proximity's
// adjacent-different-block false merges.
func TestBenchmark_ASTGroupingAccuracy(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "corpus.json"))
	require.NoError(t, err)
	var cases []benchCase
	require.NoError(t, json.Unmarshal(data, &cases))
	require.GreaterOrEqual(t, len(cases), 20, "corpus should be a meaningful size")

	dir := t.TempDir()
	g := NewGrouper(dir)
	defer func() { _ = g.Close() }()

	var (
		positives, negatives                     int
		astTP, astFP                             int
		proxTP, proxFP                           int
		astFixedLargeDrift, proxMissedLargeDrift int
	)

	for _, c := range cases {
		// Each case gets its own file path (sources differ per case).
		path := filepath.Join(dir, c.Name+"_"+c.File)
		require.NoError(t, os.WriteFile(path, []byte(c.Source), 0o644))
		rel := c.Name + "_" + c.File

		ka := g.GroupKey(reconcile.Finding{File: rel, Line: c.ALine})
		kb := g.GroupKey(reconcile.Finding{File: rel, Line: c.BLine})
		astGrouped := ka != "" && ka == kb

		// Incumbent: same file (always true here) and within ±3 lines.
		proxGrouped := abs(c.ALine-c.BLine) <= 3
		largeDrift := abs(c.ALine-c.BLine) > 3

		if c.SameBlock {
			positives++
			if astGrouped {
				astTP++
			}
			if proxGrouped {
				proxTP++
			}
			if largeDrift {
				if astGrouped {
					astFixedLargeDrift++
				}
				if !proxGrouped {
					proxMissedLargeDrift++
				}
			}
		} else {
			negatives++
			if astGrouped {
				astFP++
				t.Logf("AST false-merge on negative case %q", c.Name)
			}
			if proxGrouped {
				proxFP++
			}
		}
	}

	require.Greater(t, positives, 0, "corpus must contain same-block positives to compute recall")
	astRecall := float64(astTP) / float64(positives)
	astPrecision := 1.0
	if astTP+astFP > 0 {
		astPrecision = float64(astTP) / float64(astTP+astFP)
	}
	proxRecall := float64(proxTP) / float64(positives)

	t.Logf("positives=%d negatives=%d", positives, negatives)
	t.Logf("AST:       recall=%.3f precision=%.3f (TP=%d FP=%d)", astRecall, astPrecision, astTP, astFP)
	t.Logf("Proximity: recall=%.3f             (TP=%d FP=%d)", proxRecall, proxTP, proxFP)
	t.Logf("AST resolved %d/%d large-drift (>3) same-block pairs proximity missed", astFixedLargeDrift, proxMissedLargeDrift)

	// AC3 functional criterion: resolve ≥95% of same-block drift discrepancies.
	require.GreaterOrEqual(t, astRecall, 0.95, "AST recall on same-block pairs below 95%")
	// Precision: AST must not over-merge genuinely-different blocks.
	require.Equal(t, 0, astFP, "AST false-merged a different-block pair")
	// Dominance: AST resolves every large-drift positive proximity misses.
	require.Equal(t, proxMissedLargeDrift, astFixedLargeDrift, "AST must resolve all large-drift pairs proximity misses")
	// AST must be no worse than proximity on precision (proximity false-merges adjacent different blocks).
	require.LessOrEqual(t, astFP, proxFP, "AST precision must be at least as good as proximity")
}

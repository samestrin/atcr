package reconcile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTier4FixAnchorLossMetrics pins the observability contract for the three
// FIX-set fidelity losses: the set can be abandoned whole (anchor cap, or a
// call-scan loss that left no member behind) or narrowed (imprecise members
// dropped), and each path must increment its own counter. Without them a run
// that lost every secondary anchor to one undecidable span is indistinguishable
// in production telemetry from a FIX that simply named nothing, and a
// wrong-file PathSuggestion or a lost one cannot be attributed after the fact.
func TestTier4FixAnchorLossMetrics(t *testing.T) {
	kata := string([]rune{0x30C7, 0x30FC, 0x30BF}) // データ
	han := string([]rune{0x89E3, 0x6790})          // 解析
	genuine := kata + "_" + han                    // データ_解析

	capFix := "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `hEight` `iNine`"
	silencedFix := string([]rune{0x8A2D, 0x5B9A, 0x005F}) + "loadFile() - see `retryOnce`"
	gluedFix := "call `parseTree` instead of " + genuine + "()"

	beforeCapped := metrics.Counter(tier4FixSetCappedMetric).Value()
	beforeUnaccounted := metrics.Counter(tier4FixSetUnaccountedMetric).Value()
	beforeDropped := metrics.Counter(tier4FixAnchorDroppedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		"pkg/tree.go":   "package pkg\n\nfunc parseTree() error { return nil }\n",
		"pkg/retry.go":  "package pkg\n\nfunc retryOnce() error { return nil }\n",
		"pkg/shared.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	problem := "the `sharedHelper` path drops the returned error"
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|"+problem+"|"+capFix+"|correctness|10|ev|greta\n"+
			"HIGH|internal/ghost/phantom.go:4|"+problem+"|"+silencedFix+"|correctness|10|ev|greta\n"+
			"HIGH|internal/ghost/phantom.go:5|"+problem+"|"+gluedFix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 3)

	assert.Equal(t, beforeCapped+1, metrics.Counter(tier4FixSetCappedMetric).Value(),
		"a FIX set abandoned to the anchor cap must be counted")
	assert.Equal(t, beforeUnaccounted+1, metrics.Counter(tier4FixSetUnaccountedMetric).Value(),
		"a FIX set abandoned to a member-less fidelity loss must be counted")
	assert.Equal(t, beforeDropped+1, metrics.Counter(tier4FixAnchorDroppedMetric).Value(),
		"each imprecise member dropped from a narrowed FIX set must be counted")
}

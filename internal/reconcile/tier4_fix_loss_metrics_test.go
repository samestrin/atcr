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

// TestTier4FixSetAllDroppedMetric pins the FOURTH set-level loss, the one that
// had no signal of its own.
//
// scanFixAnchors' `len(out) == 0` arm abandons the FIX set WHOLE — it returns
// nil, identically to the capped and unaccounted arms — but leaves capped=false
// and unaccounted=false. validate.go's switch therefore fell to `default` and
// incremented only atcr_tier4_fix_anchor_dropped_total, whose documented meaning
// is "the set is narrowed rather than abandoned". A run where NO secondary
// anchor survived at all read in telemetry as one narrowed anchor: the exact
// ambiguity the set-level counters were added to remove.
//
// `設定を解析_処理()` is the shape: spaceless prose glued to a snake_case call
// name yields a single imprecise anchor, the narrowing drops it, and nothing is
// left. The per-anchor counter must NOT move for it — sharing would leave the
// two states indistinguishable again, in the other direction.
func TestTier4FixSetAllDroppedMetric(t *testing.T) {
	glued := string([]rune{0x8A2D, 0x5B9A, 0x3092, 0x89E3, 0x6790, 0x005F, 0x51E6, 0x7406}) // 設定を解析_処理
	allDroppedFix := glued + "() is wrong"

	usable, scan := scanFixAnchors(allDroppedFix)
	require.Empty(t, usable, "the narrowing removed the last member")
	require.Equal(t, []string{glued}, scan.anchors, "the scan did record one anchor")
	require.False(t, scan.capped, "the cap did not fire")
	require.False(t, scan.unaccounted, "the loss left a member behind, so it is not the member-less arm")

	beforeAllDropped := metrics.Counter(tier4FixSetAllDroppedMetric).Value()
	beforeDropped := metrics.Counter(tier4FixAnchorDroppedMetric).Value()
	beforeCapped := metrics.Counter(tier4FixSetCappedMetric).Value()
	beforeUnaccounted := metrics.Counter(tier4FixSetUnaccountedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		"pkg/shared.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|the `sharedHelper` path drops the returned error|"+
			allDroppedFix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	assert.Equal(t, beforeAllDropped+1, metrics.Counter(tier4FixSetAllDroppedMetric).Value(),
		"the set was abandoned whole: a set-level arm must be reached")
	assert.Equal(t, beforeDropped, metrics.Counter(tier4FixAnchorDroppedMetric).Value(),
		"the per-anchor counter means a NARROWED set and must not absorb an abandoned one")
	assert.Equal(t, beforeCapped, metrics.Counter(tier4FixSetCappedMetric).Value(),
		"the cap did not fire")
	assert.Equal(t, beforeUnaccounted, metrics.Counter(tier4FixSetUnaccountedMetric).Value(),
		"a loss that left a member behind is not the member-less arm")
}

// TestTier4FixSetLossesAreNotExclusive pins that the two SET-LEVEL losses are
// independent counters, not a first-match classification.
//
// A finding can be both: the anchor cap fires on eleven backticked names while a
// silenced span in the same FIX leaves a loss with no member to point at. Under
// the `switch { case capped: ...; case unaccounted: ... }` only the capped
// counter fired, which made atcr_tier4_fix_set_unaccounted_total a LOWER BOUND
// with nothing saying so — while the unavailable/incomplete pair in the same
// docs/metrics.md both-increments on the same shape (symbolindex.go's readFiles
// / complete pair). Two metric families in one document following opposite rules
// is what an operator reading either one gets wrong.
func TestTier4FixSetLossesAreNotExclusive(t *testing.T) {
	eleven := "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `hEight` `iNine` `jTen` `kEleven`"
	silenced := string([]rune{0x8A2D, 0x5B9A, 0x005F}) + "loadFile()" // 設定_loadFile()
	bothFix := eleven + " and " + silenced

	_, scan := scanFixAnchors(bothFix)
	require.True(t, scan.capped, "eleven named anchors exceed maxAnchorsPerFinding")
	require.True(t, scan.unaccounted, "the silenced span is a loss with no member to point at")

	beforeCapped := metrics.Counter(tier4FixSetCappedMetric).Value()
	beforeUnaccounted := metrics.Counter(tier4FixSetUnaccountedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		"pkg/shared.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|the `sharedHelper` path drops the returned error|"+
			bothFix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	assert.Equal(t, beforeCapped+1, metrics.Counter(tier4FixSetCappedMetric).Value(),
		"the cap fired and must be counted")
	assert.Equal(t, beforeUnaccounted+1, metrics.Counter(tier4FixSetUnaccountedMetric).Value(),
		"the member-less loss fired too: each loss increments its OWN counter, as the comment claims")
}

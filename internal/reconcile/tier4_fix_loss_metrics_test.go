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

// TestTier4FixSetUnaccountedMetricRequiresASetToAbandon pins the emptiness guard
// on the unaccounted arm, matching the one its sibling arm has always carried.
//
// atcr_tier4_fix_set_unaccounted_total is documented as "the FIX anchor set was
// abandoned whole". The all-dropped arm below it will not make that claim without
// `len(fixScan.anchors) > 0` first — you cannot abandon a set that never existed.
// The unaccounted arm made it unconditionally, so a FIX whose ONLY span is a
// silenced one (scan.anchors empty, nothing ever collected) incremented an
// abandonment counter for an abandonment that could not have happened.
//
// The cost is not cosmetic: the five FIX-loss counters exist to be SUMMED into an
// estimate of suggestions lost to anchor fidelity, and this population inflates
// that sum by findings that never had a suggestion to lose.
//
// Re-measured after the clean-reconciliation repair, which shrank this population
// but did not close it: `pkg.設定_a() returns nil` still yields scan.anchors=[]
// with unaccounted=true, because nothing in that text vouches for the name.
func TestTier4FixSetUnaccountedMetricRequiresASetToAbandon(t *testing.T) {
	settei := string([]rune{0x8A2D, 0x5B9A}) // 設定
	emptyFix := "pkg." + settei + "_a() returns nil"

	usable, scan := scanFixAnchors(emptyFix)
	require.Nil(t, usable, "precondition: the set is nil-ed by the unaccounted arm")
	require.True(t, scan.unaccounted, "precondition: the loss is genuinely unaccounted")
	require.Empty(t, scan.anchors,
		"precondition: the scan collected NOTHING - there was never a set to abandon")

	beforeUnaccounted := metrics.Counter(tier4FixSetUnaccountedMetric).Value()
	beforeAllDropped := metrics.Counter(tier4FixSetAllDroppedMetric).Value()
	beforeDropped := metrics.Counter(tier4FixAnchorDroppedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		"pkg/shared.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|the `sharedHelper` path drops the returned error|"+
			emptyFix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	assert.Equal(t, beforeUnaccounted, metrics.Counter(tier4FixSetUnaccountedMetric).Value(),
		"a FIX that never had an anchor set cannot have had one abandoned whole: "+
			"counting it here is exactly the over-count that stops the FIX-loss "+
			"counters from being summable")
	assert.Equal(t, beforeAllDropped, metrics.Counter(tier4FixSetAllDroppedMetric).Value(),
		"and it must not silently land in the sibling set-level arm either")
	assert.Equal(t, beforeDropped, metrics.Counter(tier4FixAnchorDroppedMetric).Value(),
		"nor in the per-anchor arm: no anchor was dropped, because none was collected")
}

// TestTier4FixSetContradictedMetric pins the FOURTH withhold path on the
// PathWarning-without-PathSuggestion rendering.
//
// tier4ProblemSetUnaccountedMetric's own doc argues that without it, that
// rendering conflates THREE meanings. There is a fourth on the same path: the
// contradicts() veto. locate(secondary) succeeds under a matched primary, a
// narrowed-out anchor turns out to be declared in exactly one OTHER file, the
// suggestion is withheld, resolve returns tier4Inconclusive, and nothing on the
// finding changes. So the PROBLEM-side counter took the ambiguity from four
// meanings to three, not to one.
//
// It is only INDIRECTLY attributable today: a non-empty droppedSecondary implies
// atcr_tier4_fix_anchor_dropped_total fired, but that counter fires for every
// narrowing, so it cannot isolate the veto. The before/after assertions below
// pin exactly that distinction - the drop counter moves for the narrowing, and
// the new counter moves for the veto, and neither stands in for the other.
func TestTier4FixSetContradictedMetric(t *testing.T) {
	kata := string([]rune{0x30C7, 0x30FC, 0x30BF}) // データ
	han := string([]rune{0x89E3, 0x6790})          // 解析
	glued := kata + "_" + han                      // データ_解析

	// The FIX names a survivor that localizes AND a glued span whose token is
	// narrowed out as imprecise - the dropped anchor the veto reads.
	fix := "call `parseTree` instead of " + glued + "()"
	usable, scan := scanFixAnchors(fix)
	require.NotEmpty(t, usable, "precondition: a survivor remains to localize on")
	require.NotEmpty(t, scan.droppedFixAnchors(),
		"precondition: a narrowed-out anchor rides along as veto evidence")

	beforeContradicted := metrics.Counter(tier4FixSetContradictedMetric).Value()
	beforeDropped := metrics.Counter(tier4FixAnchorDroppedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		// The PROBLEM subject, declared TWICE. That is what puts the finding on
		// the secondary branch at all: locate(primary) skips an anchor declared
		// in more than one file as "too common to localize" and returns false,
		// while the presence scan still sets primaryMatched. A subject declared
		// once would have resolved at locate(primary) and never reached the veto.
		"pkg/shared.go":  "package pkg\n\nfunc sharedHelper() error { return nil }\n",
		"pkg/shared2.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
		// The survivor locate(secondary) resolves to.
		"pkg/tree.go": "package pkg\n\nfunc parseTree() error { return nil }\n",
		// The dropped anchor, declared in exactly one OTHER file: the disagreement.
		"other/glued.go": "package other\n\nfunc " + glued + "() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|the `sharedHelper` path drops the returned error|"+
			fix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	assert.Empty(t, res.JSONFindings()[0].PathSuggestion,
		"precondition: the veto withheld the suggestion locate(secondary) produced")
	assert.Equal(t, beforeContradicted+1, metrics.Counter(tier4FixSetContradictedMetric).Value(),
		"the veto produced a file and withheld it: without its own counter this arm "+
			"renders identically to tier4Inconclusive and to a no-match on a truncated set")
	assert.Equal(t, beforeDropped+1, metrics.Counter(tier4FixAnchorDroppedMetric).Value(),
		"the drop counter still counts the NARROWING, which is a different event - "+
			"it is the precondition of the veto, never evidence the veto fired")
}

// TestTier4ProblemSetUnaccountedMetric_FixUnaccountedDoesNotSuppressThePrimaryProducer
// pins the scope of the counter's LOWER-BOUND disclosure.
//
// The row's lower-bound sentence must be read as scoping to the sub-case where
// the PROBLEM set localizes NOTHING: there, an unaccounted FIX collapses
// scanFixAnchors to nil, the secondary branch cannot fire, resolve yields
// tier4Inconclusive, and this arm is never reached. But resolve consults
// locate(primary) FIRST and returns tier4Resolved the moment it succeeds, so
// for a FIX-unaccounted finding whose PROBLEM set DOES localize, this arm IS
// reached and the counter DOES fire. A sentence that stated the suppression
// unconditionally described behavior resolve does not have.
func TestTier4ProblemSetUnaccountedMetric_FixUnaccountedDoesNotSuppressThePrimaryProducer(t *testing.T) {
	settei := string([]rune{0x8A2D, 0x5B9A}) // 設定

	problem := "`sharedHelper` drops errors; pkg." + settei + "_a() loses them"
	fix := "pkg." + settei + "_b() returns nil"

	// Precondition: the PROBLEM scan carries an unaccounted loss AND a precise
	// survivor, and the FIX scan is unaccounted with nil usable anchors, so the
	// secondary branch can never fire on this finding.
	_, problemScan := scanProblemAnchors(problem)
	require.True(t, problemScan.unaccounted, "precondition: the PROBLEM set carries an unaccounted loss")
	require.Equal(t, []string{"sharedHelper"}, problemScan.anchors,
		"precondition: the PROBLEM set localizes on the surviving precise anchor")
	fixAnchors, fixScan := scanFixAnchors(fix)
	require.True(t, fixScan.unaccounted, "precondition: the FIX is unaccounted too")
	require.Nil(t, fixAnchors, "precondition: scanFixAnchors abandoned the FIX set whole")

	before := metrics.Counter(tier4ProblemSetUnaccountedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		"pkg/shared.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|"+problem+"|"+fix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	assert.Empty(t, res.JSONFindings()[0].PathSuggestion,
		"the completeness gate still withholds the suggestion the PROBLEM survivors produced")
	assert.Equal(t, before+1, metrics.Counter(tier4ProblemSetUnaccountedMetric).Value(),
		"locate(primary) resolved before the secondary branch was ever consulted: "+
			"an unaccounted FIX does NOT keep this arm from being reached, so the "+
			"counter fires and the row must scope its lower-bound sentence to the "+
			"sub-case where the PROBLEM set did not localize")
}

// TestTier4FixSetUnaccountedMetricSkipsACleanlyCitedName is the FIX-side
// telemetry half of the clean reconciliation, asserted on the counter itself
// rather than left to follow from scanFixAnchors' return.
//
// The two are not the same claim. `unaccounted=false` is a scan-level fact; what
// an operator reads is the counter, and the arm that increments it lives in
// validate.go behind its own conjuncts. Asserting only the scan value would let
// the counter drift from the scan it is supposed to report on — which is the
// class of defect this whole epic exists to close.
func TestTier4FixSetUnaccountedMetricSkipsACleanlyCitedName(t *testing.T) {
	settei := string([]rune{0x8A2D, 0x5B9A}) // 設定
	name := settei + "_a"
	// The destroyed name is cited in backticks in the SAME text, so the loss is
	// reconciled away and the intact precise anchor survives the scan.
	fix := "`" + name + "` is broken; pkg." + name + "() returns nil"

	usable, scan := scanFixAnchors(fix)
	require.NotEmpty(t, usable, "precondition: the set is no longer abandoned whole")
	require.False(t, scan.unaccounted, "precondition: the loss is reconciled against clean")

	beforeUnaccounted := metrics.Counter(tier4FixSetUnaccountedMetric).Value()
	beforeAllDropped := metrics.Counter(tier4FixSetAllDroppedMetric).Value()

	root := gitRepoWithSources(t, map[string]string{
		"pkg/shared.go": "package pkg\n\nfunc sharedHelper() error { return nil }\n",
		"pkg/a.go":      "package pkg\n\nfunc " + name + "() error { return nil }\n",
	})
	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|the `sharedHelper` path drops the returned error|"+
			fix+"|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	assert.Equal(t, beforeUnaccounted, metrics.Counter(tier4FixSetUnaccountedMetric).Value(),
		"the name the break destroyed is spelled out faithfully two words earlier: "+
			"nothing was lost that is unknowable, so the abandonment counter must stay flat")
	assert.Equal(t, beforeAllDropped, metrics.Counter(tier4FixSetAllDroppedMetric).Value(),
		"and the surviving anchor must not be narrowed away into the sibling arm either")
}

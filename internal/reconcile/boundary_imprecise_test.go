package reconcile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/stream"
)

// TestScanAnchors_BoundaryTruncatedCallAnchorIsImprecise pins epic 35.16.6.8.2 T1
// at the unit level: a call-shape run the spaceless-script word boundary cut short
// contributes its surviving fragment as an IMPRECISE anchor, not a clean one.
//
// The fragment may be the real name (`ParseConfig` really is declared somewhere)
// or it may be the Latin tail of a mixed name the reviewer wrote whole
// (`配置ParseConfig`), and nothing in the text separates the two — which is the
// same undecidability the GLUED span is already marked imprecise for, reached by
// the boundary rather than by an underscore.
//
// `truncated` is deliberately NOT asserted true here. That flag feeds the
// no-match direction, and this epic changes only the PathSuggestion direction;
// extractAnchorSet's doc records the false reading as accepted for this shape.
func TestScanAnchors_BoundaryTruncatedCallAnchorIsImprecise(t *testing.T) {
	cases := []struct {
		name          string
		text          string
		wantAnchors   []string
		wantImprecise []string
		wantTruncated bool
	}{
		{
			name:          "latin tail after a spaceless prefix is imprecise",
			text:          "配置ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: []string{"ParseConfig"},
		},
		{
			// The disclosed accepted qualifier gap: a QUALIFIED call whose prose
			// and name share one script fires no boundary at all (設定 is the
			// qualifier discarded by the trailing-segment reduction, and ._解析
			// never breaks between scripts), so _解析 records as a clean anchor
			// with no imprecision and truncated stays false. Confirmed as a real,
			// currently-unpinned gap (resolve-td --apply-answers, 95%): a
			// boundary-rule widening that started firing on same-script prose
			// would otherwise pass the whole package green while silently
			// changing this shape's classification.
			name:          "same-script qualified call records no imprecision",
			text:          "設定._解析() ignores the returned error",
			wantAnchors:   []string{"_解析"},
			wantImprecise: nil,
			wantTruncated: false,
		},
		{
			name:          "a clean citation of the same name retracts the imprecision",
			text:          "配置ParseConfig() ignores the error returned by `ParseConfig`",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: nil,
		},
		{
			name:          "an ordinary ASCII call shape stays precise",
			text:          "BuildFileIndex() is called once per finding",
			wantAnchors:   []string{"BuildFileIndex"},
			wantImprecise: nil,
		},
		{
			name:          "a spaced-out prefix is not a boundary break",
			text:          "the helper ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: nil,
		},
		{
			// A break landing in the QUALIFIER truncates nothing that survives
			// trailingSegment: `cfg.` is discarded either way, so the recorded
			// `ParseConfig` is exactly the callee the reviewer wrote. This is the
			// most common shape in CJK review prose, and a first cut of this epic
			// marked it imprecise — withholding a CORRECT suggestion on every one
			// of them, which is the plan's own named risk.
			name:          "a break inside the qualifier of a qualified call stays precise",
			text:          "配置cfg.ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: nil,
		},
		{
			// The same shape with the break inside the recorded segment itself.
			// The qualifier is present in both rows, so the pair isolates WHERE
			// the break landed as the thing that decides the answer.
			name:          "a break inside the trailing segment of a qualified call is cut",
			text:          "cfg.配置ParseConfig() ignores the returned error",
			wantAnchors:   []string{"ParseConfig"},
			wantImprecise: []string{"ParseConfig"},
		},
		{
			name: "a silenced boundary span still contributes nothing",
			// `_ParseConfig` leads with an underscore once the break lands, so
			// collectCallAnchors silences it outright — the imprecision producer
			// added here must not resurrect it as an anchor.
			text:          "配置_ParseConfig() ignores the returned error",
			wantAnchors:   nil,
			wantImprecise: nil,
			wantTruncated: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scanAnchors(tc.text)
			assert.Equal(t, tc.wantAnchors, s.anchors)

			got := make([]string, 0, len(s.imprecise))
			for tok := range s.imprecise {
				got = append(got, tok)
			}
			assert.ElementsMatch(t, tc.wantImprecise, got)
			assert.Equal(t, tc.wantTruncated, s.truncated(),
				"this epic changes the PathSuggestion direction only, never the no-match one")
		})
	}
}

// TestRunReconcile_BoundaryTruncatedAnchorWithholdsSuggestionEndToEnd is epic
// 35.16.6.8.2's AC1 acceptance test, run against the real pipeline: a real git
// repo, the real `git ls-files` candidate index, and the real embedded parser.
//
// The tree declares ONLY the Latin tail — `ParseConfig` in
// internal/cfg/parse.go, the tail the boundary rule leaves behind. Whether the
// mixed name `配置ParseConfig` is also declared somewhere is irrelevant to the
// asserted outcome, measured: the fixture ran identically with and without a
// file declaring it, so no such file is present here (a decorative fixture file
// reads as load-bearing and gets preserved by readers trusting the comment).
// Before this epic the tail located internal/cfg/parse.go in exactly one file
// and validate.go stamped a CONFIDENT PathSuggestion there — a wrong answer at
// the one seam symbolindex.go states nothing downstream can undo.
//
// The finding must be KEPT either way. Withholding the suggestion costs a
// "did you mean" clause; routing the finding out would delete a real finding and
// durably charge the reviewer a phantom, so the outcome asserted here is
// tier4Inconclusive, never tier4NoMatch.
func TestRunReconcile_BoundaryTruncatedAnchorWithholdsSuggestionEndToEnd(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/cfg/parse.go": "package cfg\n\n" +
			"func ParseConfig() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tokens/renewal.go:31|配置ParseConfig() ignores the returned error|check the error|correctness|20|ev|greta\n")

	// The counter bracket is this test's positive control (resolve-td
	// --apply-answers, 90%): every field assertion below — PathValid false, a
	// warning, no suggestion, nothing sidecar-routed — is ALSO what a dead
	// resolver, a failed parse, or an empty anchor set produces, so without the
	// bracket the test cannot tell "the barring withheld it" from "Tier 4 never
	// reached a verdict". The bracket proves the run reached the barred-primary
	// arm and incremented exactly once (measured DELTA=1: the FIX text carries no
	// anchors, so only the primary-path site fires), mirroring the pattern in
	// TestTier4ProblemAnchorImpreciseMetric's first subtest.
	before := metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value()
	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1, "the finding is KEPT — withholding a suggestion never routes one out")
	assert.Equal(t, before+1, metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value(),
		"the AC1 mechanism fired: the barred tail, not some upstream refusal, withheld the suggestion")

	got := res.JSONFindings()[0]
	assert.Equal(t, "internal/tokens/renewal.go", got.File, "suggest-only: File is never rewritten")
	assert.False(t, got.PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, got.PathWarning)
	assert.Empty(t, got.PathSuggestion,
		"the boundary-truncated tail may not source a confident suggestion at the file declaring only that tail")

	unresolved, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	assert.Empty(t, unresolved, "an inconclusive Tier 4 verdict is never sidecar-routed")
	assert.Zero(t, res.Summary.UnresolvedFiltered)
}

// TestRunReconcile_GluedProblemAnchorStillResolvesEndToEnd is the guard on the
// narrowing epic 35.16.6.8.2 added: it bars a BOUNDARY-CUT anchor from sourcing a
// suggestion and must NOT bar a GLUED one.
//
// collectCallAnchors' doc commits to exactly that distinction — "the anchor is
// still contributed — dropping it would take the `データ_解析` direction back
// out" — and a first cut of this epic barred both, silently retracting that
// commitment with no test objecting. A glued token is the WHOLE run, so a file
// declaring that entire token is evidence the reading was right; a boundary-cut
// token is a proper suffix of a name whose prefix was destroyed, and a file
// declaring only the suffix is never such evidence.
func TestRunReconcile_GluedProblemAnchorStillResolvesEndToEnd(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/jp/parse.go": "package jp\n\nfunc データ_解析() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tokens/renewal.go:31|データ_解析() ignores the returned error|check the error|correctness|20|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	got := res.JSONFindings()[0]
	assert.Equal(t, "internal/jp/parse.go", got.PathSuggestion,
		"a glued anchor is imprecise, not unsourceable: the genuine reading must still resolve")
}

// TestScanAnchors_GluedAndBoundaryCutAreDistinctKinds pins the same distinction
// at the unit level, where the two kinds are actually recorded. Merging them into
// one flag is the change this test exists to fail.
func TestScanAnchors_GluedAndBoundaryCutAreDistinctKinds(t *testing.T) {
	glued := scanAnchors("データ_解析() ignores the returned error")
	require.Equal(t, []string{"データ_解析"}, glued.anchors)
	assert.Equal(t, impreciseGlued, glued.imprecise["データ_解析"])
	assert.Nil(t, glued.boundaryCutAnchors(),
		"a glued anchor may still SOURCE a suggestion — only its ability to delete a finding was removed")

	cut := scanAnchors("配置ParseConfig() ignores the returned error")
	require.Equal(t, []string{"ParseConfig"}, cut.anchors)
	assert.Equal(t, impreciseBoundaryCut, cut.imprecise["ParseConfig"])
	assert.Equal(t, []string{"ParseConfig"}, cut.boundaryCutAnchors(),
		"a boundary-cut anchor is a proper suffix of what the reviewer wrote and may never source one")
}

// TestTier4ProblemAnchorImpreciseMetric pins the counter epic 35.16.6.8.2 added.
// Without it, deleting the whole increment arm left the suite GREEN — and that
// counter is the arm's ONLY signal: a withheld suggestion changes no field and
// renders identically to "could not check", to a no-match on a truncated set, and
// to the FIX-side veto. Every sibling Tier-4 counter is pinned the same way.
//
// The two rows are the arm's two halves: the shape that fires it, and the veto
// shape the catalog says is deliberately silent because the unnarrowed locate()
// refused on the same disagreement anyway.
func TestTier4ProblemAnchorImpreciseMetric(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/cfg/parse.go": "package cfg\n\nfunc ParseConfig() error { return nil }\n",
		"pkg/tree.go":           "package pkg\n\nfunc readTree() error { return nil }\n",
	})

	t.Run("a barred primary resolution is counted", func(t *testing.T) {
		before := metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value()

		reviewDir := t.TempDir()
		writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
			"HIGH|internal/ghost/phantom.go:3|配置ParseConfig() ignores the returned error|check it|correctness|10|ev|greta\n")

		res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(),
			Root:         root,
		})
		require.NoError(t, err)
		require.Len(t, res.Findings, 1)
		require.Empty(t, res.JSONFindings()[0].PathSuggestion)

		assert.Equal(t, before+1, metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value(),
			"the unnarrowed set would have localized internal/cfg/parse.go; barring it must leave a signal")
	})

	t.Run("a faithful anchor that localizes anyway leaves it flat", func(t *testing.T) {
		before := metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value()

		reviewDir := t.TempDir()
		// `readTree` is cited cleanly and localizes on its own, so the barred
		// tail cost nothing: the unnarrowed locate() saw the same two files
		// disagree and refused too.
		writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
			"HIGH|internal/ghost/phantom.go:4|`readTree` and 配置ParseConfig() disagree|check both|correctness|10|ev|greta\n")

		res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
			ReconciledAt: time.Unix(1700000000, 0).UTC(),
			Root:         root,
		})
		require.NoError(t, err)

		// A flat counter is the DEFAULT of every failure mode — a dead resolver,
		// a finding routed out before this arm, an anchor scan that produced
		// nothing — so flatness alone cannot distinguish "the arm was reached and
		// stayed silent" from "this run never reached the arm". These two
		// assertions are the positive control the header comment describes: the
		// run produced exactly one finding and it DID carry a barred member (the
		// counter subtest above proves the arm fires on this shape), so the
		// silence below is meaningful. Verified: with boundaryCutAnchors killed,
		// this subtest stayed green before the control was added.
		require.Len(t, res.Findings, 1, "the run must reach resolution with its finding intact for the flatness below to mean anything")
		assert.Empty(t, res.JSONFindings()[0].PathSuggestion,
			"the faithful readTree suggestion is stamped; the barred tail contributes nothing")

		assert.Equal(t, before, metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value(),
			"the catalog promises this arm is silent where the barring changed no answer")
	})
}

// TestSymbolIndexResolve_BarredPrimaryVetoesTheSecondaryFile pins the second half
// of the barring: a barred PROBLEM anchor may not SOURCE a file and must still
// REFUSE one the FIX set produced that it disagrees with.
//
// Without the veto the epic merely SWAPS one confident answer for another —
// measured, the barred `ParseConfig` (internal/cfg/parse.go) alongside a FIX
// naming `readTree` (pkg/tree.go) stamped pkg/tree.go, where the unbarred call
// had stamped internal/cfg/parse.go. The Success Criterion asks for a withheld
// suggestion, not a differently-wrong one.
func TestSymbolIndexResolve_BarredPrimaryVetoesTheSecondaryFile(t *testing.T) {
	x := &symbolIndex{
		complete: true,
		byName: map[string][]string{
			"ParseConfig": {"internal/cfg/parse.go"},
			"readTree":    {"pkg/tree.go"},
		},
		present: map[string]uint8{"ParseConfig": presenceSource, "readTree": presenceSource},
	}

	file, outcome := x.resolve([]string{"ParseConfig"}, nil, []string{"readTree"}, nil)
	require.Equal(t, tier4Resolved, outcome, "unbarred, the primary localizes on its own")
	require.Equal(t, "internal/cfg/parse.go", file)

	file, outcome = x.resolve([]string{"ParseConfig"}, []string{"ParseConfig"}, []string{"readTree"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome,
		"a barred anchor declared in one OTHER file is the disagreement locate refuses on, secondary included")
	assert.Empty(t, file)

	// Both increment sites fire on exactly this input, and the counter is each
	// arm's ONLY signal — a withheld suggestion leaves no field change — so the
	// second site's Inc() is otherwise deletable with the suite green (measured:
	// removing it left ./internal/reconcile/ passing). Site 1 counts the narrowed
	// primary refusing a set that would have localized; site 2 counts the barred
	// veto of the FIX-sourced file. docs/metrics.md promises "one finding can
	// increment it twice"; this bracket is that promise's only pin.
	before := metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value()
	file, outcome = x.resolve([]string{"ParseConfig"}, []string{"ParseConfig"}, []string{"readTree"}, nil)
	assert.Equal(t, tier4Inconclusive, outcome)
	assert.Equal(t, before+2, metrics.Counter(tier4ProblemAnchorImpreciseMetric).Value(),
		"site 1 (primary narrowed away a localizable set) and site 2 (the barred veto) both fire on this input")
	assert.Empty(t, file)
}

// TestSymbolIndexResolve_NonSubsetBarredAnchorIsIgnored pins resolve's defensive
// clamp: barredPrimary is documented as a SUBSET of primary, and a caller that
// violates it must not buy a veto from an anchor that is not part of the set at
// all. Without the clamp, contradicts() consulted every barred member
// unconditionally, so a barred name declared in exactly one OTHER file vetoed a
// resolution the presence check and the no-match arm never saw a barred member
// for. Production cannot reach this state (boundaryCutAnchors walks the anchor
// set primary was built from) — that is exactly why the contract needs pinning
// at the boundary rather than in a caller.
func TestSymbolIndexResolve_NonSubsetBarredAnchorIsIgnored(t *testing.T) {
	x := &symbolIndex{
		complete: true,
		byName: map[string][]string{
			"anchorA": {"pkg/a.go"},
			"barredB": {"pkg/b.go"},
		},
		present: map[string]uint8{"anchorA": presenceSource, "barredB": presenceSource},
	}

	// Verified against the unclamped code: this returned ("", tier4Inconclusive)
	// — barredB vetoed a file it was never part of the set for.
	file, outcome := x.resolve([]string{"anchorA"}, []string{"barredB"}, nil, nil)
	assert.Equal(t, tier4Resolved, outcome,
		"a barred name outside primary is ignored: it may not veto what the set itself resolves")
	assert.Equal(t, "pkg/a.go", file)
}

// TestBoundaryCutAnchors_DoesNotWithholdOnAbandonedArms pins boundaryCutAnchors'
// documented behaviour on the capped and unaccounted arms: it does NOT withhold
// there, mirroring the guard structure TestDroppedFixAnchors_AbandonedArmsWithhold
// pins for its FIX-side sibling. The PROBLEM set is never abandoned — it is the
// evidence the no-match verdict rests on — so its boundary-cut members stay
// meaningful on a capped or unaccounted scan, and a future edit adding the
// sibling's `s.capped || s.unaccounted` guard here must fail. Verified against
// the gap: adding exactly that guard left the whole package green before this
// test existed.
func TestBoundaryCutAnchors_DoesNotWithholdOnAbandonedArms(t *testing.T) {
	populated := func(capped, unaccounted bool) anchorScan {
		return anchorScan{
			anchors:     []string{"dataParse", "treeWalk"},
			capped:      capped,
			unaccounted: unaccounted,
			imprecise:   map[string]anchorImprecision{"dataParse": impreciseBoundaryCut},
		}
	}

	t.Run("a narrowed scan reports its boundary-cut members", func(t *testing.T) {
		assert.Equal(t, []string{"dataParse"}, populated(false, false).boundaryCutAnchors(),
			"the baseline the two arms below must match, or the test proves nothing")
	})

	t.Run("a capped scan still reports them", func(t *testing.T) {
		assert.Equal(t, []string{"dataParse"}, populated(true, false).boundaryCutAnchors(),
			"the PROBLEM set is never abandoned: its barred members ride along on a capped scan too")
	})

	t.Run("an unaccounted scan still reports them", func(t *testing.T) {
		assert.Equal(t, []string{"dataParse"}, populated(false, true).boundaryCutAnchors(),
			"a member-less loss abandons only the FIX set; the PROBLEM set's evidence stands")
	})

	t.Run("both at once still reports them", func(t *testing.T) {
		assert.Equal(t, []string{"dataParse"}, populated(true, true).boundaryCutAnchors(),
			"neither arm withholds here, unlike the FIX-side sibling")
	})
}

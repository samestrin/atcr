package reconcile

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/samestrin/atcr/internal/stream"
	reclib "github.com/samestrin/atcr/reconcile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTier4 is a tier4Resolver whose verdict is scripted per anchor, so the
// wiring in validateFindingPaths can be tested without parsing anything.
type fakeTier4 struct {
	byAnchor map[string]string // anchor -> resolved file
	inconc   map[string]bool   // anchor -> report inconclusive
	docNamed map[string]bool   // anchor -> named only in a documentation file
	calls    int
	// buildState is what state() reports. Zero value means "applied": a scripted
	// resolver always answers, which is what a fully-in-force index looks like.
	// Tests that need a degraded build set it explicitly.
	buildState string
}

// state satisfies tier4Resolver for Summary.UnresolvedState. A scripted resolver
// has no real build, so it reports applied unless a test says otherwise.
func (f *fakeTier4) state() string {
	if f.buildState != "" {
		return f.buildState
	}
	return reclib.UnresolvedStateApplied
}

// namedInDocs satisfies tier4Resolver. A scripted resolver has no index, so it
// reports the ordinary case (a no-match means the anchor is nowhere at all)
// unless a test scripts docNamed.
//
// It mirrors the production quantifier rather than answering on the first
// docNamed hit: *lazySymbolIndex grants the shield only when EVERY anchor is
// accounted for in the tree, so an anchor a test left out of docNamed stands for
// one named nowhere and denies the answer. A fake that said "any" would let a
// multi-anchor wiring test pass against behaviour production does not have.
func (f *fakeTier4) namedInDocs(anchors []string) bool {
	if len(anchors) == 0 {
		return false
	}
	for _, a := range anchors {
		if !f.docNamed[a] {
			return false
		}
	}
	return true
}

// resolveWithDropped satisfies tier4Resolver with a per-anchor script.
//
// It mirrors the dropped-anchor veto too: a scripted resolver that ignored
// droppedSecondary would let a wiring regression (validate.go passing nil, or
// not passing the narrowed-out members at all) pass against behaviour
// production does not have.
//
// It mirrors WHERE production applies that veto, which is the SECONDARY half
// only. symbolIndex.resolve returns the moment locate(primary) succeeds —
// droppedSecondary is not consulted on that path at all, and contradicts is
// reached only from the secondary branch. A single loop over
// `append(primary, secondary...)` ran the check on whichever anchor matched
// first, so a primary hit could be vetoed and the fake was STRICTER than the
// code it stands in for: a wiring test written against it would pass while
// asserting behaviour production does not have. Pinned by
// TestFakeTier4_MirrorsResolveOnThePrimaryPath.
func (f *fakeTier4) resolveWithDropped(_ context.Context, primary, secondary, droppedSecondary []string) (string, tier4Outcome) {
	f.calls++
	if len(primary) == 0 {
		return "", tier4Inconclusive
	}
	for _, a := range primary {
		if f.inconc[a] {
			return "", tier4Inconclusive
		}
		if file, ok := f.byAnchor[a]; ok {
			return file, tier4Resolved // production returns here without reading droppedSecondary
		}
	}
	for _, a := range secondary {
		if f.inconc[a] {
			return "", tier4Inconclusive
		}
		file, ok := f.byAnchor[a]
		if !ok {
			continue
		}
		for _, d := range droppedSecondary {
			if other, ok := f.byAnchor[d]; ok && other != file {
				return "", tier4Inconclusive // a dropped name disagrees
			}
		}
		return file, tier4Resolved
	}
	return "", tier4NoMatch
}

// withFakeTier4 swaps the Tier 4 index constructor for the duration of a test
// and reports how many times one was CONSTRUCTED — the AC5 signal, since
// construction is what precedes any wazero work.
func withFakeTier4(t *testing.T, f *fakeTier4) *int {
	t.Helper()
	built := 0
	prev := newTier4Index
	newTier4Index = func(root string, paths []string) tier4Resolver {
		built++
		return f
	}
	t.Cleanup(func() { newTier4Index = prev })
	return &built
}

// tier4Repo builds a throwaway git repo (the candidate index reads
// `git ls-files`) holding the given tracked files.
func tier4Repo(t *testing.T, relpaths ...string) string {
	t.Helper()
	return gitRepoWithFiles(t, relpaths...)
}

// TestTier4_PromotesDissimilarFilename is AC1: the cited file does not exist and
// has no filename-level candidate (5.4 Tiers 1-3 all miss on a dissimilar name),
// but the construct the finding describes resolves to exactly one tracked file.
func TestTier4_PromotesDissimilarFilename(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/tokens/renewal.go",
		Line:    31,
		Problem: "`RefreshToken` never checks the expiry before reissuing",
		Fix:     "compare against the issued-at claim first",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.False(t, findings[0].PathValid)
	assert.Equal(t, stream.PathNotFoundWarning, findings[0].PathWarning)
	assert.Equal(t, "internal/auth/session.go", findings[0].PathSuggestion)
	assert.Equal(t, "internal/tokens/renewal.go", findings[0].File, "AC7 of 5.4 still holds: File is never rewritten")
	assert.Empty(t, unresolved, "a resolved finding is never sidecar-routed")
	assert.Equal(t, 1, *built)
}

// TestTier4_DocShieldRoutingIsStampedWithItsReason pins the wiring half of the
// durable-accounting fix: a finding routed because its subject was named ONLY in
// a documentation file must carry that reason on the record, and an ordinary
// no-match must not.
//
// The reason exists for the scorecard alone. Routing is correct in both cases —
// a construct is declared in source, never in prose — but only the second is
// evidence the reviewer invented something, and only the second may be charged
// to a denominator nothing ever reads back.
func TestTier4_DocShieldRoutingIsStampedWithItsReason(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go", "CHANGELOG.md")

	t.Run("named only in documentation", func(t *testing.T) {
		fake := &fakeTier4{docNamed: map[string]bool{"quantumFlux": true}}
		withFakeTier4(t, fake)

		findings := []JSONFinding{{
			File:    "internal/tokens/renewal.go",
			Line:    31,
			Problem: "`quantumFlux` never checks the expiry before reissuing",
			Fix:     "compare against the issued-at claim first",
		}}
		unresolved, _ := validateFindingPaths(context.Background(), findings, root)

		require.Equal(t, []int{0}, unresolved, "the routing itself is unchanged")
		assert.Equal(t, UnresolvedReasonDocShield, findings[0].UnresolvedReason,
			"a routing that rests on the doc-extension heuristic must say so")
	})

	t.Run("named nowhere at all", func(t *testing.T) {
		fake := &fakeTier4{}
		withFakeTier4(t, fake)

		findings := []JSONFinding{{
			File:    "internal/tokens/renewal.go",
			Line:    31,
			Problem: "`quantumFlux` never checks the expiry before reissuing",
			Fix:     "compare against the issued-at claim first",
		}}
		unresolved, _ := validateFindingPaths(context.Background(), findings, root)

		require.Equal(t, []int{0}, unresolved)
		assert.Empty(t, findings[0].UnresolvedReason,
			"a true no-match carries no reason: it is the ordinary case, and it IS chargeable")
	})
}

// TestTier4_NoMatchIsSidecarEligible is AC3's input condition: all four tiers
// exhaust with zero symbol correspondence, so the finding is reported back for
// sidecar routing.
func TestTier4_NoMatchIsSidecarEligible(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Line:    9,
		Problem: "`quantumFlux` leaks a handle on every retry",
		Fix:     "close it in `quantumFlux`",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.Equal(t, []int{0}, unresolved)
	assert.Empty(t, findings[0].PathSuggestion, "a no-match never invents a suggestion")
}

// TestTier4_InconclusiveStaysInPrimary pins the clarified rule that AC7 beats
// the plan's "fall through to step 4" prose: an anchor matching more than one
// file is real code, so the finding keeps its place in the primary stream with
// no suggestion.
func TestTier4_InconclusiveStaysInPrimary(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go", "internal/net/pool.go")
	fake := &fakeTier4{inconc: map[string]bool{"Close": true}}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "`Close` is called twice on the same handle",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.Empty(t, unresolved)
	assert.Empty(t, findings[0].PathSuggestion)
}

// TestTier4_NotConsultedWhenTiers123Suggested pins the gating: Tier 4 runs only
// when the filename-level tiers produced nothing, and never overwrites their
// answer.
func TestTier4_NotConsultedWhenTiers123Suggested(t *testing.T) {
	root := tier4Repo(t, "internal/auth/validate.go")
	fake := &fakeTier4{byAnchor: map[string]string{"ValidatePath": "internal/net/pool.go"}}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/auth/validator.go", // Tier 2 typo hit
		Problem: "`ValidatePath` swallows the error",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.Equal(t, "internal/auth/validate.go", findings[0].PathSuggestion, "the Tier 2 answer stands")
	assert.Empty(t, unresolved)
	assert.Zero(t, fake.calls, "Tier 4 is not consulted once Tiers 1-3 answered")
	assert.Zero(t, *built)
}

// TestTier4_NotConsultedForValidPaths is AC5: a run whose findings all cite real
// files has no Tier-4-eligible finding, so no index is ever constructed and the
// wazero runtime is never reached.
func TestTier4_NotConsultedForValidPaths(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/auth/session.go",
		Problem: "`RefreshToken` is fine but slow",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.True(t, findings[0].PathValid)
	assert.Empty(t, unresolved)
	assert.Zero(t, *built, "AC5: no Tier 4 index is constructed when nothing is eligible")
	assert.Zero(t, fake.calls)
}

// TestTier4_DisabledByEnv is AC6: the AST-grouping opt-out disables Tier 4 the
// same way it disables clustering — degrading to 5.4's Tier 1-3-only behavior,
// not erroring, and routing nothing to the sidecar (with no Tier 4 there is no
// evidence a finding is fabricated).
func TestTier4_DisabledByEnv(t *testing.T) {
	t.Setenv(astGroupingDisabledEnv, "1")

	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "`RefreshToken` never checks the expiry",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.False(t, findings[0].PathValid, "Tier 1-3 existence validation still runs")
	assert.Equal(t, stream.PathNotFoundWarning, findings[0].PathWarning)
	assert.Empty(t, findings[0].PathSuggestion)
	assert.Empty(t, unresolved, "nothing is sidecar-routed when Tier 4 is off")
	assert.Zero(t, *built)
}

// TestTier4_NoAnchorsIsNeverSidecarEligible pins the clarified distinction: a
// finding whose prose names no identifier could not be checked, which is not the
// same as having been checked and found nothing.
func TestTier4_NoAnchorsIsNeverSidecarEligible(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "this file is too long and hard to follow",
		Fix:     "split it up",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.Empty(t, unresolved)
	assert.Empty(t, findings[0].PathSuggestion)
}

// TestTier4_DegradesWithoutGitIndex pins that a non-git root (nil candidate
// index) keeps 5.0 existence-only behavior and never reaches Tier 4: without a
// tracked file set there is no tree to have searched.
func TestTier4_DegradesWithoutGitIndex(t *testing.T) {
	root := t.TempDir() // no git repo here
	require.NoError(t, os.WriteFile(filepath.Join(root, "real.go"), []byte("package x\n"), 0o644))

	fake := &fakeTier4{}
	built := withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Problem: "`quantumFlux` leaks a handle",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.Equal(t, stream.PathNotFoundWarning, findings[0].PathWarning)
	assert.Empty(t, unresolved)
	assert.Zero(t, *built)
}

// TestTier4_TruncatedFixAnchorSetYieldsNoSuggestion pins that the FIX
// truncation flag is honoured the way the PROBLEM one is: the searched anchor
// set was a PREFIX of what the reviewer named, and a partial search cannot
// ground a suggestion — so a truncated FIX contributes no secondary anchors at
// all rather than suggesting from its prefix.
func TestTier4_TruncatedFixAnchorSetYieldsNoSuggestion(t *testing.T) {
	root := tier4Repo(t, "internal/auth/session.go")
	fake := &fakeTier4{byAnchor: map[string]string{"RefreshToken": "internal/auth/session.go"}}
	withFakeTier4(t, fake)

	findings := []JSONFinding{{
		File:    "internal/ghost/phantom.go",
		Line:    9,
		Problem: "`ghostThing` leaks a handle on every retry",
		// Nine anchors: sorted, RefreshToken lands inside the kept prefix of
		// eight, so an implementation that searches the truncated set WOULD
		// resolve — which is exactly what must not happen.
		Fix: "route through `RefreshToken` then `anchorA` `anchorB` `anchorC` `anchorD` `anchorE` `anchorF` `anchorG` `anchorH`",
	}}
	unresolved, _ := validateFindingPaths(context.Background(), findings, root)

	assert.Empty(t, findings[0].PathSuggestion,
		"a truncated FIX anchor set is a prefix — no suggestion may be drawn from it")
	assert.Equal(t, []int{0}, unresolved,
		"the PROBLEM anchor still matched nothing: sidecar-eligible")
}

// TestFakeTier4_MirrorsResolveOnThePrimaryPath pins the fake against the
// production resolver on the one input where the two disagreed.
//
// fakeTier4.resolveWithDropped walked `append(primary, secondary...)` in ONE
// loop and ran the droppedSecondary disagreement check on whichever anchor
// matched first, so a PRIMARY hit could be vetoed. symbolIndex.resolve returns
// the moment locate(primary) succeeds — droppedSecondary is not consulted on
// that path at all, and contradicts is reached only from the secondary branch.
// The fake was therefore STRICTER than the code it stands in for, while its own
// doc claimed it "mirrors the dropped-anchor veto", which is what made the
// divergence easy to trust.
//
// No assertion was self-guaranteed by it today, because no test scripts a
// primary hit alongside a disagreeing dropped anchor. The risk is the NEXT
// wiring test written against the fake: it would pass while asserting behaviour
// production does not have. This test scripts exactly that input and requires
// both resolvers to answer alike.
//
// The table since covers the two divergences that survived that repair in the
// SAME method, both making the fake LOOSER than production — the direction that
// lets a wiring test assert a resolution the code refuses:
//
//   - the primary loop returned the FIRST byAnchor hit, while locate() refuses
//     when two precise anchors DISAGREE (symbolindex.go locate, AC7)
//   - the secondary loop had no equivalent of the primaryMatched gate, so it
//     resolved on a secondary hit where production refuses to let the FIX set
//     substitute for an absent subject — the verdict inversion that gate blocks
//
// One row per divergence is not the guard; comparing the two resolvers on the
// same input is. The fake now delegates localization to a real symbolIndex, so
// these rows pin that delegation rather than three hand-copied rules.
func TestFakeTier4_MirrorsResolveOnThePrimaryPath(t *testing.T) {
	const (
		fileA = "pkg/a.go"
		fileB = "pkg/b.go"
	)

	cases := []struct {
		name      string
		byName    map[string][]string
		primary   []string
		secondary []string
		dropped   []string
		// wantOutcome is asserted of PRODUCTION first, so a row cannot silently
		// become a comparison of two identical wrong answers.
		wantOutcome tier4Outcome
		wantFile    string
	}{
		{
			// The original row: a primary hit alongside a disagreeing dropped
			// anchor. symbolIndex.resolve returns the moment locate(primary)
			// succeeds, so droppedSecondary is never consulted there.
			name: "a primary hit is not vetoed by a disagreeing dropped anchor",
			byName: map[string][]string{
				"subjectName": {fileA},
				"droppedName": {fileB},
			},
			primary:     []string{"subjectName"},
			dropped:     []string{"droppedName"},
			wantOutcome: tier4Resolved,
			wantFile:    fileA,
		},
		{
			// Divergence (a): two PRECISE primary anchors declared in different
			// files. locate() refuses rather than picking one; the fake returned
			// whichever the loop reached first.
			name: "two disagreeing precise primary anchors resolve to nothing",
			byName: map[string][]string{
				"subjectName": {fileA},
				"otherName":   {fileB},
			},
			primary:     []string{"subjectName", "otherName"},
			wantOutcome: tier4Inconclusive,
			wantFile:    "",
		},
		{
			// Divergence (b): a secondary-only hit with no primary anchor
			// present anywhere. Production refuses to let the FIX set stand in
			// for an absent subject — rendering "did you mean X?" for a finding
			// whose subject is fabricated is the verdict inversion the no-match
			// direction exists to catch.
			name: "a secondary hit cannot substitute for an absent subject",
			byName: map[string][]string{
				"helperName": {fileB},
			},
			primary:     []string{"absentSubject"},
			secondary:   []string{"helperName"},
			wantOutcome: tier4NoMatch,
			wantFile:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			production := &symbolIndex{complete: true, byName: tc.byName}
			prodFile, prodOutcome := production.resolve(tc.primary, tc.secondary, tc.dropped)
			require.Equal(t, tc.wantOutcome, prodOutcome, "production outcome")
			require.Equal(t, tc.wantFile, prodFile, "production file")

			byAnchor := map[string]string{}
			for name, files := range tc.byName {
				require.Len(t, files, 1, "the fake's byAnchor can only express a precise anchor")
				byAnchor[name] = files[0]
			}
			fake := &fakeTier4{byAnchor: byAnchor}
			fakeFile, fakeOutcome := fake.resolveWithDropped(
				context.Background(), tc.primary, tc.secondary, tc.dropped)

			assert.Equal(t, prodOutcome, fakeOutcome,
				"a fake that diverges from production lets a wiring test assert behaviour the code does not have")
			assert.Equal(t, prodFile, fakeFile, "both resolvers name the same file")
		})
	}
}

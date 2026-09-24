package reconcile

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nineAnchorProblem is epic 35.16.6.8.2 T2's fixture: eight names the reviewer
// marked up explicitly, plus a ninth the scanner recovered from a bare call shape
// welded to spaceless prose. Nine candidates against a cap of eight, so exactly
// one must lose.
const nineAnchorProblem = "`aOne` `bTwo` `cThree` `dFour` `eFive` `fSix` `gSeven` `zLastThing` " +
	"在配置中调用ParseConfig()"

// TestExtractAnchors_CapPrefersDelimitedAnchors pins T2: the anchor cap drops the
// anchor recovered from a bare call shape before it drops one the reviewer
// backticked.
//
// UTF-8 sorts every CJK-prefixed token after all ASCII, so before the
// spaceless-script boundary rule the pseudo-token `在配置中调用ParseConfig` was
// always the first thing a plain lexical cap dropped. The boundary rule rewrites
// it to its ASCII tail, which sorts to the FRONT and survives — evicting the
// lexically-last name the reviewer actually wrote. Ordering by codepoint answers
// "which anchor sorts last", when the question the cap is really asking is "which
// anchor is the reviewer least likely to have meant".
//
// The RETURNED slice stays lexically sorted. Provenance decides only WHICH
// anchors survive; scanProblemAnchors's documented "deduped and lexically
// sorted" contract (which extractAnchorSet flattens through unchanged) still
// holds, and TestExtractAnchors_Deterministic still passes unmodified.
func TestExtractAnchors_CapPrefersDelimitedAnchors(t *testing.T) {
	got, truncated := extractAnchorSet(nineAnchorProblem)

	assert.Equal(t, []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "zLastThing"}, got,
		"the backticked names survive the cap, and the returned slice is still lexically sorted")
	assert.True(t, truncated, "the cap fired, so the set is a strict subset of what the text named")
}

// TestExtractAnchors_CapEvictsImpreciseBeforeAFaithfulCallShape pins the
// no-backtick half of the provenance rule: a BARRED boundary-cut anchor must not
// rank as a faithful member of the call-shape class. Nine call-shape candidates
// against a cap of eight — 配置ParseConfig() is barred (the spaceless-script
// boundary cut its name down to a tail that may not be what the reviewer wrote),
// the eight zzCallN() calls are read faithfully — so the cap must evict the
// barred member, not the lexically-last faithful one.
//
// Before the third provenance rank this fixture returned [ParseConfig zzCallA..
// zzCallG]: "ParseConfig" sorts before every "zzCallN" by codepoint, so the
// lexical tiebreak inside the single call-shape class kept the dead weight and
// evicted the one anchor that genuinely sources a file.
func TestExtractAnchors_CapEvictsImpreciseBeforeAFaithfulCallShape(t *testing.T) {
	problem := "配置ParseConfig()"
	for _, n := range []string{"zzCallA", "zzCallB", "zzCallC", "zzCallD", "zzCallE", "zzCallF", "zzCallG", "zzCallH"} {
		problem += " " + n + "()"
	}

	got, truncated := extractAnchorSet(problem)

	require.True(t, truncated, "nine call-shape candidates against a cap of eight: the cap must have fired")
	assert.Equal(t,
		[]string{"zzCallA", "zzCallB", "zzCallC", "zzCallD", "zzCallE", "zzCallF", "zzCallG", "zzCallH"}, got,
		"every faithful call-shape anchor survives; the barred boundary-cut tail is evicted first")
	assert.NotContains(t, got, "ParseConfig",
		"an imprecise anchor is the cap's first eviction, ahead of every faithful call shape")
}

// TestExtractAnchors_CapIsStillLexicalWithinOneProvenance guards the other half of
// the same rule: within a single provenance class nothing changed, so a finding
// naming only backticked identifiers is capped exactly as it was before.
func TestExtractAnchors_CapIsStillLexicalWithinOneProvenance(t *testing.T) {
	problem := ""
	for _, n := range []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight", "iNine"} {
		problem += "`" + n + "` "
	}
	got, truncated := extractAnchorSet(problem)

	assert.Equal(t, []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight"}, got,
		"nine delimited anchors are one provenance class: the cap keeps the lexically-first eight, as before")
	assert.True(t, truncated)
}

// TestExtractAnchors_CapPrefersDelimitedOverAnUngluedCall extends the rule past
// the shape that motivated it: provenance is DELIMITED vs CALL-SHAPE, not
// ASCII vs non-ASCII. An ordinary unglued ASCII call is still a name the reviewer
// did not mark up, so it loses to nine backticked ones just as the glued call does.
func TestExtractAnchors_CapPrefersDelimitedOverAnUngluedCall(t *testing.T) {
	problem := ""
	for _, n := range []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "zLastThing"} {
		problem += "`" + n + "` "
	}
	problem += "and BuildFileIndex() is called once per finding"

	got, truncated := extractAnchorSet(problem)
	// The cap FIRING is the precondition the claims below rest on: without it,
	// NotContains(BuildFileIndex) would also pass if the bare call were never a
	// candidate at all. Removing the call clause from the fixture must fail here.
	require.True(t, truncated, "nine candidates against a cap of eight: the cap must have fired, or the eviction claims below prove nothing")
	assert.NotContains(t, got, "BuildFileIndex",
		"a bare call shape is evicted before a name the reviewer marked up")
	assert.Contains(t, got, "zLastThing")
}

// TestRunReconcile_CapPrefersDelimitedAnchorEndToEnd is AC3's second half against
// the real pipeline: with the backticked `zLastThing` surviving the cap, the
// finding resolves to the file declaring it rather than losing its suggestion to a
// pseudo-token's ASCII tail.
func TestRunReconcile_CapPrefersDelimitedAnchorEndToEnd(t *testing.T) {
	root := gitRepoWithSources(t, map[string]string{
		"internal/other/z.go":   "package other\n\nfunc zLastThing() error {\n\treturn nil\n}\n",
		"internal/cfg/parse.go": "package cfg\n\nfunc ParseConfig() error {\n\treturn nil\n}\n",
	})

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/tokens/renewal.go:31|"+nineAnchorProblem+"|check the error|correctness|20|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)
	require.Len(t, res.Findings, 1)

	got := res.JSONFindings()[0]
	assert.Equal(t, "internal/other/z.go", got.PathSuggestion,
		"the surviving backticked anchor localizes the finding; the evicted call-shape tail must not")
}

// TestExtractAnchors_CapReturnsLexicalOrderAcrossProvenanceClasses pins the half
// of T2 the fixtures above cannot reach: that provenance decides only WHICH
// anchors survive, never how the survivors are ORDERED.
//
// In every other fixture here the survivors are one provenance class, so
// provenance order and codepoint order agree and a missing re-sort is invisible.
// This one is deliberately built so they DISAGREE — the surviving call-shape
// anchor is lexically FIRST, so a returned slice in provenance order would put it
// LAST. scanProblemAnchors's doc promises "deduped and lexically sorted", and
// consumers read the slice; leaking the cap's ranking into it would be a silent
// contract change.
func TestExtractAnchors_CapReturnsLexicalOrderAcrossProvenanceClasses(t *testing.T) {
	problem := ""
	for _, n := range []string{"bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight"} {
		problem += "`" + n + "` "
	}
	problem += "then aOne() and zLastCall() run"

	got, truncated := extractAnchorSet(problem)

	require.True(t, truncated, "nine candidates against a cap of eight: the cap must have fired")
	require.Len(t, got, maxAnchorsPerFinding)
	assert.Equal(t,
		[]string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight"}, got,
		"the seven marked-up names plus the lexically-first call shape survive, returned in plain lexical order")
	assert.NotContains(t, got, "zLastCall", "the lexically-last call-shape anchor is the one the cap drops")
}

// TestScanAnchors_CapCanRetractACleanCallShapeVouch pins the coupling between the
// provenance cap and reconcileSilenced's `anchors` conjunct, which nothing else
// in the suite holds.
//
// reconcileSilenced retracts a silence only when the destroyed token is BOTH in
// `clean` AND survives into the POST-cap anchor set, so which anchors the cap
// evicts decides `unaccounted`. A clean citation delivered by a BARE CALL is
// call-shape class, so it now ranks below every delimited anchor and eight
// backticked names evict it — where the old lexical cap kept it, `_解析` sorting
// ahead of every lowercase ASCII name.
//
// unaccounted=true is the INTENDED value on the capped row, not a defect: the
// vouching token is exactly what the cap removed, so it is not sitting in the set
// locate will read, and reconcileSilenced's doc states that clearing the claim for
// a token locate never sees converts a withheld suggestion into a wrong one. What
// was missing was any test that fails if the coupling is edited away — reconciling
// against the PRE-cap `clean` set, or reordering cap and reconciliation, passes
// every other anchor test in the package.
//
// The two rows differ ONLY by the eight backticked names, so the flip is
// attributable to the cap and to nothing else in the text.
func TestScanAnchors_CapCanRetractACleanCallShapeVouch(t *testing.T) {
	// `parse_解析()` breaks at the Latin/Han boundary, dropping a SPACING-script
	// prefix, so the silence is recorded against the fragment `_解析` itself.
	// The bare `_解析()` call re-cites that exact token with no boundary and no
	// glue, so it is a CLEAN contribution — and a call-shape one.
	const silencedPlusCleanCall = "parse_解析() and _解析() ok"
	eight := ""
	for _, n := range []string{"aOne", "bTwo", "cThree", "dFour", "eFive", "fSix", "gSeven", "hEight"} {
		eight += "`" + n + "` "
	}

	uncapped := scanAnchors(silencedPlusCleanCall)
	require.False(t, uncapped.capped, "one anchor against a cap of eight: the cap must not have fired")
	require.True(t, uncapped.lostSpan, "the spaceless-script break really did cost the span its prefix")
	assert.Equal(t, []string{"_解析"}, uncapped.anchors)
	assert.False(t, uncapped.unaccounted,
		"the destroyed name is cited cleanly by a bare call AND survives into the set: nothing about it is unknowable")

	capped := scanAnchors(eight + silencedPlusCleanCall)
	require.True(t, capped.capped, "nine candidates against a cap of eight: the cap must have fired")
	assert.NotContains(t, capped.anchors, "_解析",
		"a call-shape anchor ranks below every delimited one, so the vouching token is the cap's first eviction")
	assert.True(t, capped.unaccounted,
		"the vouch was evicted, so the destroyed name is NOT in the set locate reads and the loss keeps its claim")
}

// TestRunReconcile_CapEvictingTheSubjectCannotRouteAFinding records the measured
// cost of the broad provenance rule and pins the bound on it.
//
// The comparator demotes EVERY call-shape anchor below every delimited one, so a
// faithful unglued ASCII call — typically the finding's own subject — is
// deterministically the first eviction whenever eight backticked names are
// present. That is broader than the CJK pseudo-token case that motivated it, and
// epic 35.16.6.8.2 T2 decided it deliberately: a name the reviewer marked up is
// better evidence of what they meant than one recovered from a bare call shape.
//
// The measurement that makes the breadth affordable is this: the cap sets
// `capped`, `capped` feeds `truncated`, and validate.go's routing arm is gated on
// !problemTruncated. So a capped PROBLEM set can never produce tier4NoMatch
// routing, no matter which member was evicted — the subject included. What the
// eviction can still cost is a SUGGESTION, and only that; validate.go's
// unaccounted arm already discloses that it is scoped to `unaccounted` and not to
// `capped` for the same reason.
//
// This test is that bound, not a restatement of the ordering: it fails if the cap
// stops setting `capped`, if `truncated` stops reading it, or if the routing arm
// stops consulting it — any one of which turns the deliberate demotion into a
// mechanism for deleting real findings.
func TestRunReconcile_CapEvictingTheSubjectCannotRouteAFinding(t *testing.T) {
	// The tree declares the SUBJECT and none of the eight backticked names.
	root := gitRepoWithSources(t, map[string]string{
		"internal/pay/charge.go": "package pay\n\nfunc processPayment() error {\n\treturn nil\n}\n",
	})

	problem := ""
	for _, n := range []string{"absentA", "absentB", "absentC", "absentD", "absentE", "absentF", "absentG", "absentH"} {
		problem += "`" + n + "` "
	}
	problem += "are unchecked when processPayment() runs"

	scan := scanAnchors(problem)
	require.True(t, scan.capped, "nine candidates against a cap of eight: the cap must have fired")
	require.NotContains(t, scan.anchors, "processPayment",
		"the faithful call shape is the first eviction — this is the measured cost the rule accepts")
	require.True(t, scan.truncated(), "the cap must reach validate.go through truncated(), or the bound below is vacuous")

	reviewDir := t.TempDir()
	writeFindings(t, filepath.Join(reviewDir, "sources"), "greta/findings.txt",
		"HIGH|internal/ghost/phantom.go:3|"+problem+"|check them|correctness|10|ev|greta\n")

	res, err := RunReconcile(context.Background(), reviewDir, nil, Options{
		ReconciledAt: time.Unix(1700000000, 0).UTC(),
		Root:         root,
	})
	require.NoError(t, err)

	unresolved, err := ReadUnresolvedFindings(reviewDir)
	require.NoError(t, err)
	assert.Len(t, res.Findings, 1,
		"the eight surviving anchors are all absent from the tree, yet the finding is KEPT: a capped set may not answer no-match")
	assert.Empty(t, unresolved,
		"the evicted subject can cost a suggestion and nothing more — routing is gated on !problemTruncated")
}

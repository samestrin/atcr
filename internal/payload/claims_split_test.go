package payload

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// splitClaims is deterministic by construction — no model is in this path. A
// paraphrase layer between the author's assertion and the panel's adjudication
// is exactly where "the fix is described but absent" softens into "the fix is
// described", which the diff then satisfies.

func TestSplitClaims_SubjectIsAlwaysAClaim(t *testing.T) {
	got := splitClaims([]string{"fix cursor preservation in begin()"})
	require.Len(t, got, 1)
	assert.Equal(t, "fix cursor preservation in begin()", got[0])
}

func TestSplitClaims_BulletsBecomeOneClaimEach(t *testing.T) {
	msg := "fix the drain path\n\n" +
		"- begin() no longer wipes a live cursor\n" +
		"* _drain_offset() returns None on an all-malformed batch\n" +
		"+ the caller keeps its previous offset\n" +
		"1. the regression test covers begin()\n"
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"fix the drain path",
		"begin() no longer wipes a live cursor",
		"_drain_offset() returns None on an all-malformed batch",
		"the caller keeps its previous offset",
		"the regression test covers begin()",
	}, got)
}

func TestSplitClaims_ProseBodySplitsPerSentence(t *testing.T) {
	msg := "fix the drain path\n\nThe cursor is now preserved. The helper returns None instead of zero."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"fix the drain path",
		"The cursor is now preserved.",
		"The helper returns None instead of zero.",
	}, got)
}

// A version number or an abbreviation is not a sentence boundary.
func TestSplitClaims_SentenceSplitIgnoresDottedTokens(t *testing.T) {
	// The subject is scaffolding here; it only has to be claim-bearing. A bare
	// "bump deps" is filtered as a noise subject (see the noise-opener tests).
	msg := "bump the pinned tool version\n\nUpgrade to v1.2.3 across the board. No behavior change is intended."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"bump the pinned tool version",
		"Upgrade to v1.2.3 across the board.",
		"No behavior change is intended.",
	}, got)
}

// A message carrying no assertion must yield zero claims, not one noise claim
// per line: a ledger of trailers asks the panel to adjudicate metadata.
func TestSplitClaims_TrailersAndNoiseYieldNoClaims(t *testing.T) {
	msg := "wip\n\n" +
		"Signed-off-by: A Dev <dev@example.com>\n" +
		"Co-authored-by: Someone Else <other@example.com>\n" +
		"Refs: #123\n" +
		"https://example.com/pull/9\n"
	assert.Empty(t, splitClaims([]string{msg}))
}

func TestSplitClaims_FencedCodeIsNotAClaim(t *testing.T) {
	msg := "add the guard\n\n```go\nif x == nil { return }\n```\nThe guard rejects a nil cursor."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"add the guard",
		"The guard rejects a nil cursor.",
	}, got)
}

// A squashed or cherry-picked branch repeats the same subject across commits;
// enumerating it twice pads the ledger without adding an assertion.
func TestSplitClaims_ExactDuplicatesAreCollapsed(t *testing.T) {
	got := splitClaims([]string{"fix the drain path", "fix the drain path", "widen the test"})
	assert.Equal(t, []string{"fix the drain path", "widen the test"}, got)
}

func TestSplitClaims_IsByteIdenticalAcrossRuns(t *testing.T) {
	msgs := []string{
		"fix the drain path\n\n- begin() keeps the cursor\n- the helper returns None\n",
		"widen the test\n\nThe test now asserts begin(). It no longer asserts the helper.",
	}
	first := splitClaims(msgs)
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, splitClaims(msgs))
	}
}

func TestSplitClaims_EmptyInputYieldsNoClaims(t *testing.T) {
	assert.Empty(t, splitClaims(nil))
	assert.Empty(t, splitClaims([]string{"", "   ", "\n\n"}))
}

// A hard-wrapped bullet is one assertion, not two. Treating the wrap as its own
// claim files a sentence fragment the contract then demands a verdict and a
// file/line citation for.
func TestSplitClaims_WrappedBulletContinuationStaysOneClaim(t *testing.T) {
	msg := "fix the drain path\n\n" +
		"- the cursor fix preserves the offset\n" +
		"  when the drain is cold\n" +
		"- the helper returns None\n"
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"fix the drain path",
		"the cursor fix preserves the offset when the drain is cold",
		"the helper returns None",
	}, got)
}

// Commit prose routinely opens a sentence with a lowercase identifier. Requiring
// an uppercase letter after the terminator collapsed a whole body into one
// claim — on prose written in exactly the style of the defect report that
// motivated this epic.
func TestSplitClaims_SplitsSentencesThatStartLowercase(t *testing.T) {
	msg := "fix the drain\n\nbegin() still assigns zero. begin() is unchanged. the helper is added."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"fix the drain",
		"begin() still assigns zero.",
		"begin() is unchanged.",
		"the helper is added.",
	}, got)
}

// Relaxing the capital rule must not start shredding abbreviations.
func TestSplitClaims_AbbreviationsAreNotSentenceBoundaries(t *testing.T) {
	// The subject is scaffolding here; it only has to be claim-bearing. A bare
	// "bump deps" is filtered as a noise subject (see the noise-opener tests).
	msg := "bump the pinned tool version\n\nUpgrade to v1.2.3 e.g. the pinned tool. no behavior change is intended."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"bump the pinned tool version",
		"Upgrade to v1.2.3 e.g. the pinned tool.",
		"no behavior change is intended.",
	}, got)
}

// The duplicate-collapse keys `seen` on the RAW claim, but sanitizeClaim does not
// run until render time. Two raw claims that differ only in what sanitizing strips
// or rewrites — a CR versus a space, a 4-dash run versus a 7-dash run, an invalid
// UTF-8 byte — therefore survive dedup and are enumerated as two IDENTICAL
// numbered claims. That is exactly where a squashed or cherry-picked branch lands,
// and it defeats the stated purpose: enumerating a claim twice pads the ledger
// without adding an assertion.
func TestSplitClaims_CollapsesClaimsThatDifferOnlyInWhatSanitizingRemoves(t *testing.T) {
	got := splitClaims([]string{
		"subject one ---- tail",
		"subject one ------- tail",
	})
	rendered := claimLedgerSection(got, claimsComplete)
	assert.Equal(t, 1, strings.Count(rendered, "subject one -- tail"),
		"two raw claims that sanitize to the same text are one claim")
}

// The continuation test is `line != trimmed`, which is true for any line carrying
// TRAILING whitespace — including every line of a CRLF message, because splitting
// on "\n" leaves the "\r". A Windows-authored branch therefore gets a ledger that
// is present but substantively wrong: paragraph sentences after the first bullet
// are swallowed into that bullet instead of becoming their own claims. The comment
// above the test says "an INDENTED line directly under a bullet", so the fix is to
// make the code check what the comment already says.
func TestSplitClaims_CRLFBodyDoesNotSwallowParagraphsIntoTheBullet(t *testing.T) {
	got := splitClaims([]string{"subject line here\r\n\r\n- bullet claim one\r\nA separate paragraph sentence.\r\nAnother separate one.\r\n"})
	assert.Equal(t, []string{
		"subject line here",
		"bullet claim one",
		"A separate paragraph sentence.",
		"Another separate one.",
	}, got)
}

// The same defect fires on a plain LF message whose paragraph line happens to
// carry one trailing space — invisible in every editor, and it silently merges
// two assertions into one verdict.
func TestSplitClaims_TrailingSpaceDoesNotSwallowAParagraphIntoTheBullet(t *testing.T) {
	// The trailing space must not be on the LAST line: strings.TrimSpace over the
	// whole message would remove it there and hide the defect.
	got := splitClaims([]string{"subject line here\n\n- bullet claim one\nA separate paragraph sentence. \nAnother separate one.\n"})
	assert.Equal(t, []string{"subject line here", "bullet claim one", "A separate paragraph sentence.", "Another separate one."}, got)
}

// A genuinely INDENTED line under a bullet is still that bullet's continuation:
// hard-wrapped bullets are ordinary, and filing the wrap as its own claim would
// demand a verdict and a citation for a sentence fragment.
func TestSplitClaims_IndentedLineStillContinuesTheBullet(t *testing.T) {
	got := splitClaims([]string{"subject line here\n\n- bullet claim one\n  wrapped onto a second line\n"})
	assert.Equal(t, []string{"subject line here", "bullet claim one wrapped onto a second line"}, got)
}

// A ledger padded with noise trains reviewers to answer VERIFIED reflexively,
// which is how the real UNSUPPORTED gets missed. These subjects assert nothing,
// yet the contract demands a verdict and a file:line citation for each.
func TestSplitClaims_DropsNoiseSubjectsThatAssertNothing(t *testing.T) {
	for _, noise := range []string{"wip fixup", "bump deps", "WIP again", "tmp hack", "squash me", "fixup!  typo"} {
		assert.Empty(t, splitClaims([]string{noise}), "%q asserts nothing", noise)
	}
}

// The bar stays deliberately low otherwise: dropping a real claim costs a verdict
// the panel never renders, which is worse than carrying a weak one. A noise WORD
// inside a real assertion must not disqualify it.
func TestSplitClaims_KeepsShortRealClaimsAndNoiseWordsInRealSentences(t *testing.T) {
	for _, real := range []string{
		"update the parser so it preserves the offset",
		"Fixed pagination.",
		"bump the retry ceiling to 5 so a flaky upstream recovers",
	} {
		assert.Len(t, splitClaims([]string{real}), 1, "%q is a real assertion", real)
	}
}

// "No" as the English interjection is far more common in commit prose than "No."
// as an abbreviation for "number". Treating its period as part of an abbreviation
// merges two assertions into one claim, and the panel then renders one verdict
// where two were owed.
func TestSplitSentences_NoIsAnInterjectionNotAnAbbreviation(t *testing.T) {
	assert.Equal(t, []string{"No.", "The fix is wrong."}, splitSentences("No. The fix is wrong."))
}

// splitSentences treats '!' and '?' as terminators in the same branch as '.',
// but nothing exercised either character: the branch could lose them and the
// suite would stay green while two assertions collapsed into one verdict.
func TestSplitSentences_ExclamationAndQuestionAlsoEndSentences(t *testing.T) {
	assert.Equal(t, []string{"The helper is added!", "The test now passes."},
		splitSentences("The helper is added! The test now passes."))
	assert.Equal(t, []string{"Is the cursor preserved?", "It is now."},
		splitSentences("Is the cursor preserved? It is now."))
	assert.Equal(t, []string{"Really?!", "Yes."}, splitSentences("Really?! Yes."))
}

// splitSentences was quadratic on abbreviation-dense prose: when isAbbrevBefore
// returned true the loop continued WITHOUT advancing start, so the next
// abbreviation re-scanned the whole prefix — and the check itself allocated a
// full strings.Fields slice of that prefix each time. The only thing bounding it
// was DefaultMaxClaimBytes, and commitMessages documents maxBytes <= 0 as
// unlimited, so nothing bounded it on that path at all.
func TestSplitSentences_AbbreviationDenseProseIsNotQuadratic(t *testing.T) {
	// A complexity-class assertion, not a wall-clock budget: doubling the input
	// costs ~2x for a linear scan and ~4x for a quadratic one, and that ratio is
	// the same on a fast laptop and a loaded CI runner. The absolute escape hatch
	// below keeps it from comparing two sub-millisecond measurements, where timer
	// noise dominates and the ratio means nothing.
	measure := func(reps int) time.Duration {
		input := strings.Repeat("etc. ", reps)
		best := time.Duration(1<<63 - 1)
		for i := 0; i < 3; i++ {
			start := time.Now()
			_ = splitSentences(input)
			if d := time.Since(start); d < best {
				best = d
			}
		}
		return best
	}
	small := measure(1600) // ~8 KiB, exactly DefaultMaxClaimBytes
	big := measure(3200)   // twice that
	if big < 5*time.Millisecond {
		return // already linear-fast at 16 KiB; the ratio would be timer noise
	}
	assert.Less(t, float64(big)/float64(small), 3.0,
		"doubling the input more than tripled the cost: the abbreviation check is re-scanning the whole prefix per terminator (small=%s big=%s)", small, big)
}

func BenchmarkSplitSentences_AbbreviationDense(b *testing.B) {
	input := strings.Repeat("etc. ", 1600)
	for i := 0; i < b.N; i++ {
		_ = splitSentences(input)
	}
}

// "see" and "link" are ordinary English verbs before they are git trailers, so
// stripping any line that opens with them discards real assertions — re-creating
// the invisible-absence failure this epic exists to catch, and doing it with no
// truncation-style disclosure. The trailer stripper must recognise only genuine
// reference trailers.
func TestSplitClaims_SeeAndLinkOpeningARealClaimAreNotStripped(t *testing.T) {
	got := splitClaims([]string{"subject line here\n\nSee: begin() now preserves the offset\nLink: the offset is kept in drain.py"})
	assert.Equal(t, []string{
		"subject line here",
		"See: begin() now preserves the offset",
		"Link: the offset is kept in drain.py",
	}, got)
}

// The genuine reference trailers still go: they name an issue, not a change.
func TestSplitClaims_RealReferenceTrailersAreStillStripped(t *testing.T) {
	for _, trailer := range []string{"Refs: #123", "Fixes: #456", "Closes: #789", "Resolves: #1", "CC: @someone", "Bug: 4242", "Issue: 17", "PR: #99"} {
		got := splitClaims([]string{"subject line here\n\n" + trailer})
		assert.Equal(t, []string{"subject line here"}, got, "%q is a reference, not a claim", trailer)
	}
}

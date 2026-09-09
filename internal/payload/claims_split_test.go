package payload

import (
	"strings"
	"testing"

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
	msg := "bump deps\n\nUpgrade to v1.2.3 across the board. No behavior change is intended."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"bump deps",
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
	msg := "bump deps\n\nUpgrade to v1.2.3 e.g. the pinned tool. no behavior change is intended."
	got := splitClaims([]string{msg})
	assert.Equal(t, []string{
		"bump deps",
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
	rendered := claimLedgerSection(got, false)
	assert.Equal(t, 1, strings.Count(rendered, "subject one -- tail"),
		"two raw claims that sanitize to the same text are one claim")
}

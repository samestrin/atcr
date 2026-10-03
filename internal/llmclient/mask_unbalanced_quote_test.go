package llmclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// MaskJSONStrings pairs `"` bytes from offset 0 with no JSON-validity check, so an
// ODD number of quotes before a think block inverts the in/out-of-string state and
// blanks that block's TAGS. Every consumer of the mask asks a fail-closed detection
// question — "is there markup here that could be hiding a discarded draft?" — so a
// mask that hides the markup makes all three lanes ADMIT a reply they exist to
// refuse. In the debate lane that admission is durable: parseRuling takes the draft
// and applyRulings writes it onto the finding.
//
// The remedy is the one internal/reconcile already applies to its fence mask
// (TestFenceMask_UnterminatedFenceDoesNotMaskTheTail): a dangling delimiter must not
// poison the rest of the document. Here that means the mask reports nothing rather
// than reporting a guess — it returns the input unchanged, so every tag stays
// visible and the detection sites refuse.
func TestMaskJSONStrings_UnbalancedQuoteDoesNotHideMarkup(t *testing.T) {
	t.Parallel()

	// One unpaired `"` opens a literal that never closes. Everything after it —
	// including the think tags — would be blanked by a length-only mask.
	raw := `He said "it is fine. ` +
		"\x3cthink\x3e" + `{"outcome":"overturn","reasoning":"draft"}` + "\x3c/think\x3e" +
		` {"outcome":"uphold","reasoning":"real"}`

	assert.True(t, HasEnclosingThinkBlock(raw),
		"precondition: the raw reply really does carry an enclosing think block")

	assert.True(t, HasEnclosingThinkBlock(MaskJSONStrings(raw)),
		"an unbalanced quote count must not blank the think tags: the mask's in-string state is a guess "+
			"at that point, and hiding the markup makes every detection site admit the draft it exists to refuse")

	assert.Equal(t, raw, MaskJSONStrings(raw),
		"with the quote count unbalanced the mask has nothing trustworthy to say, so it must return the "+
			"input untouched rather than blank a region it only guessed was a literal")
}

// The inch-mark reproduction: an otherwise-clean JSON reply whose reasoning value
// quotes a think pair, followed by prose carrying ONE stray `"`. Discarding the
// whole masked copy (the pre-existing arm) leaves the quoted pair visible and sends
// every detection site to refusal — real signal loss on legal input, since the tag
// is a QUOTE, not markup. Masking only the balanced prefix keeps the quote hidden
// while leaving the genuinely-ambiguous suffix raw.
func TestMaskJSONStrings_InchMarkAfterBalancedReplyKeepsTheQuotedTagMasked(t *testing.T) {
	t.Parallel()

	raw := `{"verdict":"confirmed","reasoning":"the code emits ` +
		"\x3cthink\x3e" + `draft` + "\x3c/think\x3e" + ` before the answer"} note: a 6" gap`

	assert.True(t, HasEnclosingThinkBlock(raw),
		"precondition: the raw reply carries the quoted think pair")

	assert.False(t, HasEnclosingThinkBlock(MaskJSONStrings(raw)),
		"the pair sits inside a cleanly-closed literal, so the stray quote AFTER it must not un-mask the "+
			"prefix: refusing this reply is the signal loss the prefix mask exists to close")

	assert.Len(t, MaskJSONStrings(raw), len(raw),
		"the offset ClassifyUnopenedCloser computes on the masked copy is sliced out of the unmasked original")
}

// A truncated reply (cut MID-string) is odd-quoted by construction. A pair quoted
// EARLIER, inside a literal that closed cleanly, must stay masked — only the open
// literal's suffix is a guess, and it carries no tag here, so the reply parses.
func TestMaskJSONStrings_TruncatedReplyKeepsAnEarlierQuotedTagMasked(t *testing.T) {
	t.Parallel()

	truncated := `{"reasoning":"the code emits ` +
		"\x3cthink\x3e" + `draft` + "\x3c/think\x3e" + `","other":"unfinished`

	assert.True(t, HasEnclosingThinkBlock(truncated),
		"precondition: the raw reply carries the quoted think pair")

	assert.False(t, HasEnclosingThinkBlock(MaskJSONStrings(truncated)),
		"the pair is in the balanced prefix, so the unterminated tail must not un-mask it")

	assert.Len(t, MaskJSONStrings(truncated), len(truncated))
}

// The complement: when the unterminated literal itself carries a think pair, that
// suffix is a genuine guess and MUST stay raw so detection refuses. This is the arm
// the prefix mask deliberately keeps.
func TestMaskJSONStrings_UnterminatedTailStillExposesItsThinkPair(t *testing.T) {
	t.Parallel()

	// The only literal is the open one, so there is no balanced prefix to mask —
	// the whole tag-carrying region is the ambiguous suffix.
	raw := `he said "trust me ` + "\x3cthink\x3e" + `draft` + "\x3c/think\x3e" + ` ok`

	assert.True(t, HasEnclosingThinkBlock(MaskJSONStrings(raw)),
		"the tag is inside the unterminated literal, so it stays visible and detection refuses — "+
			"the mask must not blank a region whose in-string state is only a guess")

	assert.Len(t, MaskJSONStrings(raw), len(raw))
}

// The balanced case is the mask's whole purpose and must keep working: a think tag
// that appears SOLELY inside a JSON string value is a model quoting the tag while
// ruling on think-handling code, which is the likeliest input in this repo.
func TestMaskJSONStrings_BalancedQuotesStillMaskAQuotedTag(t *testing.T) {
	t.Parallel()

	quoted := `{"outcome":"uphold","reasoning":"the code emits ` +
		"\x3cthink\x3e" + `...` + "\x3c/think\x3e" + ` markup"}`

	assert.False(t, HasEnclosingThinkBlock(MaskJSONStrings(quoted)),
		"a tag confined to a string value is a quote, not markup, and the balanced-quote mask must still hide it")
}

// Length preservation is load-bearing beyond detection: ClassifyUnopenedCloser
// computes an offset on the masked copy and slices the UNMASKED original at it, so a
// mask that changed length would mis-slice the envelope. Both arms must preserve it.
func TestMaskJSONStrings_PreservesLengthOnBothArms(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]string{
		"balanced":          `{"a":"b"} plain`,
		"unbalanced":        `he said "hello there`,
		"lone backslash":    `{"a":"b\`,
		"empty":             ``,
		"multibyte in body": `{"a":"héllo wörld"} ok`,
	} {
		assert.Len(t, MaskJSONStrings(in), len(in),
			"%s: the offset ClassifyUnopenedCloser computes on the masked copy is sliced out of the "+
				"unmasked original, so the two must stay byte-aligned", name)
	}
}

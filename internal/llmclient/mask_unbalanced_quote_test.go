package llmclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A prose quote at a NON-JSON position opens no literal, so the think tags after it
// stay visible and every detection site refuses the draft they exist to catch.
//
// The claim is deliberately scoped to that position, and the scope is the point. This
// case was written as `UnbalancedQuoteDoesNotHideMarkup` and asserted the property for
// any stray quote — but its fixture's `"` follows a LETTER (`He said "`), which is not
// a JSON position, so the quote cannot open a literal BY CONSTRUCTION. The assertion
// was guaranteed by the fixture's byte position rather than by the code, and it
// certified a property across the whole input space while the real hole went untested:
// a quote after `,` `:` `{` `[` or at the start of input DOES open a literal, and the
// mask then swallows the opener while the closer survives.
//
// Mutating the position gate away does fail this case, which is what made the
// over-broad wording actively misleading rather than merely thin — it reported green
// on a property it never exercised. The other positions now live in
// TestMaskJSONStrings_ProseQuoteAtJSONPositionMustNotSplitAThinkPair, and the
// fail-closed arm they drive is in MaskJSONStrings itself
// (TD internal/llmclient/mask_unbalanced_quote_test.go:1).
func TestMaskJSONStrings_ProseQuoteAtANonJSONPositionOpensNoLiteral(t *testing.T) {
	t.Parallel()

	// The `"` follows `d` — not `{`, `[`, `,`, `:` or start-of-input — so openCtx is
	// false and no literal opens. Nothing after it is masked.
	raw := `He said "it is fine. ` +
		"\x3cthink\x3e" + `{"outcome":"overturn","reasoning":"draft"}` + "\x3c/think\x3e" +
		` {"outcome":"uphold","reasoning":"real"}`

	assert.True(t, HasEnclosingThinkBlock(raw),
		"precondition: the raw reply really does carry an enclosing think block")

	assert.True(t, HasEnclosingThinkBlock(MaskJSONStrings(raw)),
		"a prose quote at a non-JSON position must not blank the think tags: it opens no literal, so the "+
			"markup stays visible and every detection site refuses the draft it exists to catch")

	assert.Equal(t, len(raw), len(MaskJSONStrings(raw)),
		"length must be preserved on every arm: ClassifyUnopenedCloser slices the unmasked original at an "+
			"offset computed on the masked copy, so the two must stay byte-aligned")
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

// The complement: a quote opened in JSON position whose literal is never closed masks
// the literal's tail to end-of-input, so a think pair quoted inside it stays HIDDEN
// and the lane parses — the truncated-reply shape (`{...,"other":"unfinished`). Only
// a quote in JSON position opens a literal; a prose quote does not, which is the arm
// TestMaskJSONStrings_UnbalancedQuoteDoesNotHideMarkup pins.
func TestMaskJSONStrings_UnterminatedLiteralKeepsItsQuotedTagHidden(t *testing.T) {
	t.Parallel()

	raw := `{"reasoning":"trust me ` + "\x3cthink\x3e" + `draft` + "\x3c/think\x3e" + ` ok`

	assert.True(t, HasEnclosingThinkBlock(raw),
		"precondition: the raw reply carries the quoted think pair")

	assert.False(t, HasEnclosingThinkBlock(MaskJSONStrings(raw)),
		"the pair sit inside a JSON-position literal, so it is a quote and stays masked even when the "+
			"literal is never closed — the truncated reply is cut mid-string by construction")

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

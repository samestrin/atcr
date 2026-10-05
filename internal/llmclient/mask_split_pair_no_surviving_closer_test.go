package llmclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkOpen / thinkClose come from think_test.go, which builds them from escapes so a
// literal tag in test source is not picked up by the repo's own think-tag scanners.

// The split-pair routes a SURVIVING-CLOSER discriminator cannot see. Each one is the
// same defect TestMaskJSONStrings_ProseQuoteAtJSONPositionMustNotSplitAThinkPair pins —
// a prose quote at a JSON position opens a literal, and the mask then blanks a run that
// swallows the block's OPENER — reached by a reply where no exact lowercase `</think>`
// is left visible to signal it:
//
//   - the model never closes the block at all;
//   - it closes with a variant spelling, which is not a tag here by design, so
//     strings.Count never counts it;
//   - its lowercase closer is itself swallowed by a LATER literal.
//
// In all three the removal counts and the surviving-closer test both read exactly as a
// legitimately quoted lone opener does, so the mask is published, every detection site
// reads no block, and the draft object is the first keyed object every lane's parser
// finds (TD internal/llmclient/think.go:406).
//
// The discriminator that separates them from the legal shape is neither count nor
// closer: it is whether the blanked run SPANS THE DRAFT'S OWN `{`. A quoted lone opener
// sits in an ordinary string value, so the mask's closing quote is a real JSON string
// terminator and the run holds no container. A cut pair's run ends at the opening quote
// of a KEY inside the draft, so it necessarily swallowed the `{` that opened it.
func TestMaskJSONStrings_SplitPairWithNoSurvivingCloserStillDiscardsTheMask(t *testing.T) {
	t.Parallel()

	draft := `{"outcome":"overturn","reasoning":"draft, not committed"}`

	for name, raw := range map[string]string{
		"never closed": `The proposer wrote, "the guard is missing ` + thinkOpen + draft +
			` that was scratch reasoning, nothing committed.`,
		"closer in a different case": `The proposer wrote, "the guard is missing ` + thinkOpen + draft +
			` </THINK> the real answer follows.`,
		"closer with a space before the bracket": `The proposer wrote, "the guard is missing ` + thinkOpen + draft +
			` </think > the real answer follows.`,
		"lowercase closer swallowed by a later literal": `Say, "a ` + thinkOpen + draft +
			` b", c, "` + thinkClose + ` d", e, "` + thinkOpen + ` f"`,
	} {
		masked := MaskJSONStrings(raw)

		require.True(t, HasEnclosingThinkBlock(raw),
			"%s: precondition — the raw reply really does carry an opener-anchored block holding a draft", name)

		require.Len(t, masked, len(raw),
			"%s: ClassifyUnopenedCloser computes an offset on the masked copy and slices the unmasked "+
				"original at it, so the two must stay byte-aligned on this arm too", name)

		assert.True(t, HasEnclosingThinkBlock(masked),
			"%s: the blanked run spans the draft's own `{`, so the mask's closing quote was the opening "+
				"quote of a key and its boundary was a guess — no surviving closer says so, and admitting "+
				"the reply lets each lane's parser take the discarded draft as committed", name)
	}
}

// The complement, stated on its own so the arm above cannot be satisfied by bailing out
// whenever a brace appears near a tag: a `{` that precedes the quoted opener is ordinary
// value text, not a draft the mask cut into, and the reply must still parse.
func TestMaskJSONStrings_BraceBeforeAQuotedOpenerIsNotASplitPairAndStaysMasked(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"brace earlier in the same value": `{"fix":"replace {old} with ` + thinkOpen +
			` handling","explanation":"the strip is leading-only"}`,
		"brace in an earlier value": `{"note":"the shape is {a:b}","fix":"strip the ` + thinkOpen +
			` prefix before parsing"}`,
		// The reverse order, and the case that pins the per-literal reset of
		// runHasOpener. Each literal is its own run: an opener in an EARLIER value must
		// not attribute a LATER value's brace to itself. Without the reset this reply
		// refuses, and nothing else in the suite notices.
		"opener in an earlier value, brace in a later one": `{"a":"x ` + thinkOpen +
			` y","b":"z {brace}"}`,
	} {
		masked := MaskJSONStrings(raw)

		require.Len(t, masked, len(raw), "%s: length must be preserved on every arm", name)

		assert.False(t, HasEnclosingThinkBlock(masked),
			"%s: the brace sits BEFORE the opener, so the mask's closing quote really did terminate the "+
				"value — nothing was cut and the quoted tag must stay hidden", name)
	}
}

// The SECOND trigger, pinned on its own. The run-shape signature cannot see this reply:
// the quoted opener sits in a brace-free value, so nothing was swallowed that looks like
// a cut, yet a genuine markup `</think>` survives OUTSIDE any literal. HasEnclosingThinkBlock
// reads the raw reply as a block holding text, so the mask is discarded and the lanes refuse.
//
// Without this case the surviving-closer half of the arm survives deletion with the whole
// suite green — an untested guard, which is how a guard ships broken. Keeping the trigger is
// deliberate: dropping it would hand this reply to ClassifyUnopenedCloser instead, and that
// is a behaviour change wider than the row it came from
// (TD internal/llmclient/think.go:406).
func TestMaskJSONStrings_QuotedOpenerBesideASurvivingMarkupCloserStillDiscardsTheMask(t *testing.T) {
	t.Parallel()

	raw := `{"a":"x ` + thinkOpen + ` y"} ` + thinkClose + ` tail`

	require.True(t, HasEnclosingThinkBlock(raw),
		"precondition — the raw reply reads as a block holding text")

	masked := MaskJSONStrings(raw)

	require.Len(t, masked, len(raw), "length must be preserved on every arm")

	assert.True(t, HasEnclosingThinkBlock(masked),
		"a swallowed opener beside a closer that survived the mask is the residue the second trigger "+
			"exists for: the run held no container, so only the surviving closer says the mask is unsafe")
}

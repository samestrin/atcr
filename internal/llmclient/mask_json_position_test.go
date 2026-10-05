package llmclient

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkOpen / thinkClose come from think_test.go, which builds them from escapes so a
// literal tag in test source is not picked up by the repo's own think-tag scanners.

// A stray prose quote AT A JSON POSITION opens a literal, and the mask then runs from
// there until the next `"` — wherever that happens to be. When the draft ruling inside
// the think block supplies that `"` (every parseable ruling object does, by
// construction), the mask stops mid-block: it swallows the `<think>` OPENER and leaves
// the `</think>` CLOSER visible.
//
// That split is the defect. HasEnclosingThinkBlock sees no block, so the judge-reply
// guard at internal/debate/debate.go:700 admits the reply, parseRuling takes the FIRST
// outcome-keyed object — the discarded draft — and applyRulings writes that verdict
// durably onto the finding.
//
// The position rule itself is right and must be kept: it is what stopped a cleanly
// quoted pair being read as markup (refusal on legal input). What was missing is a
// fail-closed arm for the case the rule CANNOT decide — and the discriminator is not
// the quote count. `inStr` is FALSE at end-of-input on every case below, because the
// draft's own quotes re-balance the state, which is why an unbalanced-count bail-out
// does not fire here.
//
// The discriminator is directional: masking that removes MORE OPENERS THAN CLOSERS has
// demonstrably cut a pair in half. A genuinely quoted pair loses both halves together
// and must stay masked (TestMaskJSONStrings_BalancedQuotesStillMaskAQuotedTag,
// TestMaskJSONStrings_UnterminatedLiteralKeepsItsQuotedTagHidden).
func TestMaskJSONStrings_ProseQuoteAtJSONPositionMustNotSplitAThinkPair(t *testing.T) {
	t.Parallel()

	draft := thinkOpen + "\n" + `{"outcome":"overturn","reasoning":"draft, not committed"}` + "\n" + thinkClose

	// Every JSON position that sets openCtx, plus the whitespace-transparent variants.
	// A comma or a colon before a quotation is the ordinary way English introduces
	// quoted speech, so these are the common input, not edge cases.
	for name, raw := range map[string]string{
		"after a comma":            `The proposer wrote, "the guard is missing` + "\n" + draft + "\nthat was my scratch reasoning.",
		"after a colon":            `Analysis: "the handler is unsafe` + "\n" + draft + "\nthe real answer follows.",
		"after an open brace":      `{"the reviewer is wrong` + "\n" + draft + "\nreal answer.",
		"after an open bracket":    `["the reviewer is wrong` + "\n" + draft + "\nreal answer.",
		"at the start of input":    `"the guard is missing` + "\n" + draft + "\nreal answer.",
		"after a comma + newline":  "Context,\n   \"the guard is missing\n" + draft + "\nreal answer.",
		"after a colon + tab":      "Verdict:\t\"the handler is unsafe\n" + draft + "\nreal answer.",
		"after a comma, no spaces": `wrote,"the guard is missing` + "\n" + draft + "\nreal answer.",
	} {
		masked := MaskJSONStrings(raw)

		require.True(t, HasEnclosingThinkBlock(raw),
			"%s: precondition — the raw reply really does carry an enclosing think block", name)

		require.Len(t, masked, len(raw),
			"%s: ClassifyUnopenedCloser computes an offset on the masked copy and slices the unmasked "+
				"original at it, so the two must stay byte-aligned on the fail-closed arm too", name)

		// The split signature this test exists to name: without the fix the opener is
		// gone and the closer survives.
		assert.True(t, HasEnclosingThinkBlock(masked),
			"%s: a prose quote at a JSON position must not leave the mask holding half a think pair — "+
				"the opener was swallowed and the closer survived, so every detection site admits a "+
				"reply whose draft ruling parseRuling will take as committed", name)
	}
}

// The directional half, stated on its own so a future edit cannot satisfy the test
// above by bailing out indiscriminately: a pair that is genuinely quoted inside a
// literal loses BOTH halves to the mask, which is the mask's whole purpose.
func TestMaskJSONStrings_QuotedPairLosesBothHalvesAndStaysMasked(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		// The case the position rule was added for: an inch mark after a balanced reply.
		"balanced reply then an inch mark": `{"verdict":"confirmed","reasoning":"the code emits ` +
			thinkOpen + `draft` + thinkClose + ` markup"} note: a 6" gap`,
		// A truncated reply, cut mid-string by construction.
		"unterminated literal, pair inside": `{"reasoning":"trust me ` + thinkOpen + `draft` + thinkClose + ` ok`,
		// Plain balanced quoting.
		"balanced value": `{"outcome":"uphold","reasoning":"the code emits ` + thinkOpen + `...` + thinkClose + ` markup"}`,
	} {
		masked := MaskJSONStrings(raw)

		assert.False(t, strings.Contains(masked, thinkOpen),
			"%s: the opener sits inside a string value and must stay masked", name)
		assert.False(t, strings.Contains(masked, thinkClose),
			"%s: the closer sits inside the same string value and must stay masked with it — "+
				"losing both halves together is what distinguishes a quote from a split pair", name)
		assert.False(t, HasEnclosingThinkBlock(masked),
			"%s: a tag confined to a string value is a model quoting the tag, not markup", name)
	}
}

// The shape the directional arm admits by accident, and the one it must not act on: a
// reply that quotes a LONE OPENER inside a cleanly-closed string value. There is no
// pair here, so nothing was cut in half — yet the mask removes one `<think>` and zero
// `</think>`, which satisfies "more openers than closers" exactly as a split pair does.
// The two are indistinguishable on removal counts alone.
//
// The discriminator the arm actually needs is whether a CLOSER SURVIVED the mask. A
// split pair leaves one visible beside the swallowed opener — that is the dangerous
// residue the arm exists for. A lone quoted opener leaves none, so discarding the mask
// buys nothing and costs the reply: the tag becomes visible, every detection site reads
// it as markup, and all three lanes refuse a reply that is legal by the position rule's
// own definition. This is the likeliest input in this repo, where findings discuss
// think handling (TD internal/llmclient/think.go:392).
func TestMaskJSONStrings_LoneQuotedOpenerIsNotASplitPairAndStaysMasked(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]string{
		"opener named in a fix string": `{"fix":"strip the ` + thinkOpen +
			` prefix before parsing","explanation":"the strip is leading-only"}`,
		"opener named in a reasoning string": `{"verdict":"confirmed","reasoning":"the model emitted a bare ` +
			thinkOpen + ` opener and never closed it"}`,
		"two lone openers, still no closer": `{"a":"first ` + thinkOpen + ` here","b":"second ` +
			thinkOpen + ` there"}`,
	} {
		masked := MaskJSONStrings(raw)

		require.Len(t, masked, len(raw),
			"%s: ClassifyUnopenedCloser slices the unmasked original at an offset computed on the "+
				"masked copy, so the two must stay byte-aligned here too", name)

		assert.False(t, strings.Contains(masked, thinkOpen),
			"%s: the opener sits inside a cleanly-closed string value — no closer survived the mask, "+
				"so no pair was split and the fail-closed arm has nothing to protect", name)
		assert.False(t, HasEnclosingThinkBlock(masked),
			"%s: discarding the mask here refuses a legal reply — the executor lane drops the repair, "+
				"the skeptic lane returns unverifiable, and the debate lane returns unresolved", name)
	}
}

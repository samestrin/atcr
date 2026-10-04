package verify

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TD internal/verify/invoke.go:201 — the mask fix must REACH all three lanes.
//
// MaskJSONStrings is shared (internal/llmclient/think.go), but the guard it feeds is
// per-lane: the debate lane had its own end-to-end pin
// (TestRunDebate_UnbalancedQuoteBeforeAThinkBlockStillRefusesTheDraft) while the
// verify and executor lanes had NONE. Mutation-proven before this file existed:
// reverting the mask's unbalanced-quote arm failed ./internal/debate/ and left
// ./internal/verify/ fully GREEN — so nothing here proved the shared fix reached
// either lane, and a future narrowing of the mask would regress them silently.
//
// These are the twins. They exercise the lane HARNESSES end to end, not the helper,
// so the pin is on the behaviour an operator sees: the refusal note in the verify
// lane and the dropped repair in the executor lane.

// The refusal the mask exists to preserve: a reply with a prose quote at a NON-JSON
// position before a real post-answer think block. The quote follows a LETTER, so it
// opens no literal and every tag after it stays visible — that is this case's whole
// scope, and the comma/colon positions that DO open one are covered by the siblings
// below. The block encloses a discarded draft, so
// HasEnclosingThinkBlock must see it and the lane must refuse — if the mask hid the
// block's tags the first keyed object would be read as the committed verdict, which
// reconcile/gate.go then trusts durably.
func TestInvokeSkeptic_UnbalancedQuoteBeforeAPostAnswerThinkBlockStillRefusesTheDraft(t *testing.T) {
	t.Parallel()
	raw := `He said "it is fine. ` +
		"\x3cthink\x3e" + `{"verdict":"confirmed","reasoning":"draft, wrong"}` + "\x3c/think\x3e" +
		` {"verdict":"refuted","reasoning":"real answer"}`
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", finalChat(raw), okDispatcher(), false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict,
		"the stray quote must not un-mask the block: hiding its tags admits the draft as the committed verdict")
	assert.Equal(t, "think_markup_after_answer", v.Notes)
}

// The position the case above cannot reach. A COMMA-introduced prose quotation sits at
// a JSON position, so it opens a literal; the mask runs to the next `"` — supplied by
// the draft verdict object itself — and stops mid-block, swallowing the opener while
// the closer survives. The lane then admitted the reply and read the DRAFT as the
// committed verdict, which reconcile/gate.go trusts durably: a draft `refuted` clears
// the gate at any severity and is charged to the reviewer's survived_skeptic_rate
// (TD internal/llmclient/mask_unbalanced_quote_test.go:1).
func TestInvokeSkeptic_CommaIntroducedQuoteBeforeAPostAnswerThinkBlockStillRefusesTheDraft(t *testing.T) {
	t.Parallel()
	raw := "The finding claims, \"the guard is missing\n" +
		"\x3cthink\x3e" + `{"verdict":"refuted","reasoning":"draft, wrong"}` + "\x3c/think\x3e" +
		"\nThat was scratch work. " + `{"verdict":"confirmed","reasoning":"real answer"}`
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", finalChat(raw), okDispatcher(), false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictUnverifiable, v.Verdict,
		"a comma-introduced quote opens a literal, so the mask swallowed the block's opener and the "+
			"lane admitted the draft as the committed verdict")
	assert.Equal(t, "think_markup_after_answer", v.Notes)
}

// The executor twin, where a false admission is worse than in the verify lane: the
// first balanced object is returned as the fix and --auto-fix writes it to disk.
// A false REFUSAL is also costly here — it drops a valid repair entirely — which is
// why the quoted-tag companions below matter as much as this refusal.
func TestInvokeExecutor_UnbalancedQuoteBeforeAPostAnswerThinkBlockStillDropsTheRepair(t *testing.T) {
	t.Parallel()
	raw := `He said "it is fine. ` +
		"\x3cthink\x3e" + `{"fix":"DRAFT: delete the validation","explanation":"draft, wrong"}` + "\x3c/think\x3e" +
		` {"fix":"REAL: add a bounds check","explanation":"real answer"}`
	fix, warn, _ := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], finalChat(raw), okDispatcher(), 0, "")
	assert.Empty(t, fix, "the draft patch must never be returned as the fix")
	assert.Contains(t, warn, "think markup",
		"the refusal must name its cause, so an operator is not left with a silent empty fix")
}

// The executor twin of the comma position, where a false admission is worst: the first
// balanced object is returned as the fix and --auto-fix writes it to disk, so the
// model's discarded DRAFT patch would be applied to the tree
// (TD internal/llmclient/mask_unbalanced_quote_test.go:1).
func TestInvokeExecutor_CommaIntroducedQuoteBeforeAPostAnswerThinkBlockStillDropsTheRepair(t *testing.T) {
	t.Parallel()
	raw := "The finding claims, \"the guard is missing\n" +
		"\x3cthink\x3e" + `{"fix":"DRAFT: delete the validation","explanation":"draft, wrong"}` + "\x3c/think\x3e" +
		"\nThat was scratch work. " + `{"fix":"REAL: add a bounds check","explanation":"real answer"}`
	fix, warn, _ := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], finalChat(raw), okDispatcher(), 0, "")
	assert.Empty(t, fix,
		"the draft patch must never be returned as the fix -- --auto-fix would write it to the tree")
	assert.Contains(t, warn, "think markup",
		"the refusal must name its cause, so an operator is not left with a silent empty fix")
}

// The signal-loss half — the defect the mask widening introduced, and the reason
// this row exists. A think pair QUOTED inside a JSON string value is the model
// discussing think handling, and a trailing inch-mark prose quote makes the reply
// odd-quoted. The pair must stay masked so the lane PARSES the reply; un-masking it
// refuses legal input, and a TRUNCATED reply (cut mid-string) is odd-quoted by
// construction, so this is the common case, not a corner.
func TestInvokeSkeptic_QuotedTagWithATrailingUnbalancedQuoteStillParses(t *testing.T) {
	t.Parallel()
	raw := `{"verdict":"confirmed","reasoning":"the code emits ` +
		"\x3cthink\x3e" + `draft` + "\x3c/think\x3e" + ` before the answer"} note: a 6" gap`
	v, _, err := invokeSkeptic(context.Background(), testSkeptic(), "prompt", finalChat(raw), okDispatcher(), false)
	require.NoError(t, err)
	require.NotNil(t, v)
	assert.Equal(t, verdictConfirmed, v.Verdict,
		"the pair is inside a cleanly-closed literal, so the stray quote after it must not unmask it and force a refusal")
}

// Executor twin of the signal-loss half. Here the cost of the false refusal is the
// whole repair: the fix is dropped, postCheck logs executor_fix_failed, and the
// finding goes unrepaired.
func TestInvokeExecutor_QuotedTagWithATrailingUnbalancedQuoteStillParses(t *testing.T) {
	t.Parallel()
	raw := `{"fix":"REAL: add a bounds check","explanation":"the code emits ` +
		"\x3cthink\x3e" + `draft` + "\x3c/think\x3e" + `"} note: a 6" gap`
	fix, warn, _ := invokeExecutor(context.Background(), agentExecConfig(), testExecProviderVal(),
		eligibleFinding()[0], finalChat(raw), okDispatcher(), 0, "")
	assert.Equal(t, "", warn, "the quoted pair is not markup, so nothing encloses the fix")
	assert.Equal(t, "REAL: add a bounds check", fix,
		"a valid fix must not be dropped because a prose quote followed it")
}

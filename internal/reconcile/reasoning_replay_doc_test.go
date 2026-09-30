package reconcile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Sprint 35.16.11.2.2.4 T5 made the tool loop strip an inline <think> block off
// every replayed assistant turn (historyMessage, internal/fanout/loop.go). That
// changed what "the tool loop re-sends provider reasoning on every later turn"
// means in practice: before the strip, an endpoint that reasoned INLINE had its
// reasoning ride back inside Content; after it, that reasoning is dropped and
// only the four reasoning members are replayed.
//
// The two published claims that a reader consults about replay did not say so,
// which is what the Phase 5 exit gate caught. `preserve_thinking` makes the gap
// sharp: the key is legal ONLY under thinking_style qwen or glm — the
// <think>-tag families — so the one config the flag exists for is the one where
// an inline reasoner now has nothing replayed to preserve.
//
// This is the prose half of the drift pair. The behavior half is pinned in
// internal/fanout (TestToolLoop_ReplayedHistoryCarriesNoThinkBlock,
// TestToolLoop_ThinkOnlyTurnReplaysAsNullContent,
// TestToolLoop_InlineThinkWithNoReasoningMemberIsStripped); this side asserts the
// doc keeps DISCLOSING it, since a reader who trusts the old wording will
// configure an inline endpoint and see no replay.
//
// Tokens, not whole sentences: registry.md is one long line per paragraph, so a
// reworded connective must not fail a test whose subject is the claim.
func TestRegistryDoc_ReasoningReplayExcludesInlineThink(t *testing.T) {
	doc := readDoc(t, "registry.md")

	t.Run("the replay contract names the members-only scope", func(t *testing.T) {
		for _, want := range []string{
			"**Reasoning here means those four members only.**",
			"is not provider reasoning for replay purposes",
			"stripped off the assistant turn before that turn is appended",
			"so it never rides back",
			// Why the strip exists at all. Without this, a later editor reads the
			// carve-out as an oversight to "fix" by replaying Content again.
			"its own discarded draft replayed as settled prior output",
			// The consequence an operator actually observes.
			"replay carries nothing and the turn-2+ request body is unchanged",
			// The content:null normalization is the one part of this strip whose
			// output goes back on the wire, so a strict validator makes it visible.
			"replays as `content: null`, not an empty string",
		} {
			assert.Contains(t, doc, want,
				"registry.md's Reasoning replay paragraph must state the inline-think carve-out: missing %q", want)
		}
	})

	// Scoped to the row, not the file: a caveat added three rows away is not the
	// caveat a reader of THIS row will ever see.
	t.Run("the preserve_thinking row names the same carve-out", func(t *testing.T) {
		row := docTableRow(t, doc, "preserve_thinking")

		for _, want := range []string{
			"the four reasoning **members** only",
			"is stripped from the replayed assistant turn and is never sent back",
			"no replayed reasoning for this flag to preserve",
			// The row must keep saying WHY this bites here specifically.
			"legal only under `thinking_style: qwen` or `glm`",
			// A doctor `honored` verdict counts an inline block as a signal, so it
			// does not answer "did the reasoning arrive in a member?". Without this
			// pointer an operator reads `honored` as proof replay will work.
			"`honored` alone does not tell you the reasoning arrived in a member",
		} {
			assert.Contains(t, row, want,
				"the preserve_thinking row must state the inline-think carve-out: missing %q", want)
		}
	})
}

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
// Marker for the **Reasoning replay.** paragraph, unique in registry.md (the
// word "reasoning" appears on two other lines, one of them the preserve_thinking
// row, so a looser marker would pin the wrong line).
const replayParagraphMarker = "the tool loop re-sends provider reasoning on every later turn"

func TestRegistryDoc_ReasoningReplayExcludesInlineThink(t *testing.T) {
	doc := readDoc(t, "registry.md")
	paragraph := docLineContaining(t, doc, replayParagraphMarker)

	t.Run("the replay contract names the members-only scope", func(t *testing.T) {
		line := paragraph
		for _, want := range []string{
			"**Reasoning here means those four members only.**",
			"is not provider reasoning for replay purposes",
			"stripped off the assistant turn before that turn is appended",
			// Why the strip exists at all. Without this, a later editor reads the
			// carve-out as an oversight to "fix" by replaying Content again.
			"its own discarded draft replayed as settled prior output",
			// The consequence an operator actually observes.
			"replay contributes nothing",
		} {
			assert.Contains(t, line, want,
				"registry.md's Reasoning replay paragraph must state the inline-think carve-out: missing %q", want)
		}
	})

	// The qualifier is load-bearing, and the round-1 wording of this very
	// paragraph dropped it: SplitThink is leading-only, so a block after answer
	// text IS appended and DOES ride back. An unqualified "never rides back" is
	// false against historyMessage and would tell an operator the hazard is
	// eliminated when it is only narrowed (TD-008). Asserted as its own subtest so
	// the failure message says which half went wrong.
	t.Run("the replay contract keeps the leading-only qualifier", func(t *testing.T) {
		line := paragraph
		for _, want := range []string{
			"A **leading** inline `<think>…</think>` run in the reply **content**",
			"The strip is **leading-only**",
			"left in place and DOES ride back in history",
		} {
			assert.Contains(t, line, want,
				"registry.md must NOT claim an inline block never rides back: missing %q", want)
		}
		// The three positive assertions above ARE the change-sensitive half: they
		// name the qualifier a false "never rides back" would have to drop. A
		// NotContains on one SPELLING of the bad claim caught nothing — the phrase
		// it banned ("so it never rides back") does not occur in registry.md at
		// all, so it would have passed even if the paragraph had been rewritten to
		// assert the very thing it bans — and being whole-document scoped, an
		// unrelated later sentence in a 630-line file would false-fail it. Banned
		// wording is not a guard; the positive form is (see the "a DEFAULT, not a
		// floor" pattern in skeptic_budget_docs_test.go).
	})

	// The content:null normalization is the one part of this strip whose output
	// goes back on the wire, and it is WIDER than the strip: a blank Content that
	// carried no think markup is nilled too, so the turn-2+ body changes for every
	// tool-enabled agent, including a roster that declares no thinking keys. The
	// paragraph must not attribute the null to a think block alone.
	t.Run("the content-null normalization is stated as independent of thinking", func(t *testing.T) {
		line := paragraph
		for _, want := range []string{
			"replays as `content: null`, not an empty string",
			"with no think markup in it at all",
			// Conditional, not universal: historyMessage returns a nil Content
			// untouched (internal/fanout/loop.go:66-68), so a provider that already
			// sends null on a tool-call turn sees a byte-identical body. The round-2
			// wording claimed "every tool-enabled agent" and argued against its own
			// universal two clauses later, where it says null is what OpenAI requires.
			"whose provider sends an empty string there, declared thinking or not",
			"a provider that already sends `null` on such a turn is unaffected",
		} {
			assert.Contains(t, line, want,
				"registry.md must state the content:null normalization is independent of thinking: missing %q", want)
		}
	})

	// Epic 35.16.11.2.2.9 made the replay switchable per agent, so the paragraph's
	// old "they do not switch the replay on or off" became false. It must name the
	// one key that does, and must not promise it for lanes that ignore it yet.
	t.Run("the replay contract names the replay_reasoning opt-out", func(t *testing.T) {
		line := paragraph
		for _, want := range []string{
			"do not switch the replay off. The one key that does is `replay_reasoning: off`",
			"Only the review lanes honor it until slice 35.16.11.2.2.9.1",
			"the skeptic and the debate seats still replay",
		} {
			assert.Contains(t, line, want,
				"registry.md's Reasoning replay paragraph must name the replay_reasoning opt-out and its scope: missing %q", want)
		}
	})

	// Scoped to the row, not the file: a caveat added three rows away is not the
	// caveat a reader of THIS row will ever see.
	t.Run("the preserve_thinking row names the same carve-out", func(t *testing.T) {
		row := docTableRow(t, doc, "preserve_thinking")

		for _, want := range []string{
			"the four reasoning **members** only",
			// Same qualifier as the paragraph above, for the same reason: the row's
			// round-1 wording said "never sent back", which is false.
			"A **leading** inline `<think>…</think>` run in the reply **content** is stripped from the replayed assistant turn and does not ride back",
			"the strip is leading-only, so a block after answer text is left in place and is replayed",
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

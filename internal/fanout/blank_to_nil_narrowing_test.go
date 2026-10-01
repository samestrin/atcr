package fanout

// TD internal/fanout/loop.go:70 (sprint 35.16.11.2.2.4, disagreement LOW vs MEDIUM) — the
// blank-to-nil normalisation in historyMessage changes WIRE behaviour for a case with NO
// think markup at all: a provider that sends empty-string content on a tool-call turn
// previously had the empty string replayed and now gets null. The existing subtest pins the
// change deliberately, but its justification is speculative — a hypothetical strict validator
// and an unverified LiteLLM translation, with the reverse risk (a validator that rejects null)
// not considered. This is a request-path behaviour change smuggled into a think-stripping
// sprint.
//
// FIX arm 1 (the non-speculative arm): narrow the guard to fire only when the strip actually
// removed something — compare the answer's length against the original content's — keeping
// pre-sprint behaviour (empty string replayed as "") for a genuinely-empty content.
//
// RED: a genuinely-empty content must replay as "" exactly as before the sprint, and a
// think-only turn must still take the canonical content:null shape.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolLoop_GenuinelyEmptyContent_ReplaysAsEmptyString pins the narrowed guard: no think
// markup + empty content = "" on the wire, byte-for-byte the pre-sprint behaviour.
func TestToolLoop_GenuinelyEmptyContent_ReplaysAsEmptyString(t *testing.T) {
	bodies, _ := runWireToolLoopContent(t, 1, false, func(turn int) string {
		if turn == 1 {
			return ""
		}
		return "final answer"
	})
	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3, "prompt, assistant tool call, tool result")

	// Assert on the RAW JSON: json.Unmarshal("null", &string) silently succeeds,
	// so a typed decode would make this test a tautology. `""` (quoted empty
	// string) is the pre-sprint wire shape; null is the smuggled change.
	assert.Equal(t, `""`, string(msgs[1]["content"]),
		"empty string content, not null: the sprint must not change the wire shape of a case it did not strip")
}

// TestToolLoop_WhitespaceOnlyNoThinkMarkup_ReplaysAsEmptyString is the same narrowing for a
// whitespace-only content with no markup: the strip removed nothing, so the guard must not fire.
func TestToolLoop_WhitespaceOnlyNoThinkMarkup_ReplaysAsEmptyString(t *testing.T) {
	bodies, _ := runWireToolLoopContent(t, 1, false, func(turn int) string {
		if turn == 1 {
			return "   \n\t  "
		}
		return "final answer"
	})
	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3)

	assert.Equal(t, `""`, string(msgs[1]["content"]),
		"whitespace-only content with no markup replays as a string, not null — the strip removed nothing")
}

// TestToolLoop_ThinkOnlyTurnStillReplaysAsNull is the shape the sprint DID mean to change:
// content that was entirely a stripped think run takes the canonical content:null shape.
func TestToolLoop_ThinkOnlyTurnStillReplaysAsNull(t *testing.T) {
	bodies, _ := runWireToolLoopContent(t, 1, false, func(turn int) string {
		if turn == 1 {
			return "<think>I should read f1.go</think>"
		}
		return "final answer"
	})
	msgs := wireMessages(t, bodies[1])
	require.Len(t, msgs, 3)
	assert.Equal(t, "null", string(msgs[1]["content"]),
		"a turn that was entirely reasoning still takes the canonical content:null shape")
}

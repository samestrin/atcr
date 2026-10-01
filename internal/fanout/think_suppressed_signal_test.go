package fanout

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
)

// TD internal/fanout/engine.go:533 — a LEADING think opener with no canonical
// closer makes llmclient.SplitThink return the whole reply as reasoning, so
// invokeSlot records an empty answer, ParsedFindingCount 0, and the generic
// UnparseableResponse bit. The FIX asks for a DISTINCT signal — "content was
// entirely reasoning" — so a review suppressed by the think strip is
// distinguishable in status.json from a merely garbled reply.
//
// RED: ThinkSuppressed must be set when the reply carried leading think markup
// whose strip left an empty answer (the whole reply was reasoning), and must
// NOT be set for a garbled reply with no think markup at all.

// stubCompleter returns a fixed reply for every call.
type stubCompleter struct{ content string }

func (s *stubCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return s.content, nil
}

// TestInvokeSlot_ThinkOnlyReply_SetsDistinctSuppressedSignal pins the new
// ThinkSuppressed signal: a StatusOK reply whose leading think run swallowed
// the whole content is "content was entirely reasoning", not "garbled".
func TestInvokeSlot_ThinkOnlyReply_SetsDistinctSuppressedSignal(t *testing.T) {
	e := NewEngine(&stubCompleter{content: "<think>\nreasoning about the diff"})
	r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

	assert.Equal(t, StatusOK, r.Status)
	assert.Equal(t, 0, r.ParsedFindingCount())
	require.True(t, r.UnparseableResponse, "still the generic unparseable bit")
	assert.True(t, r.ThinkSuppressed,
		"the reply was entirely a think run — the distinct signal must fire so an operator sees WHY the review was suppressed")
}

// TestInvokeSlot_ThinkOnlyReplyWithTrailingNewline_SetsSuppressedSignal pins the
// ROUTINE shape the test above cannot reach. SplitThink keeps the whitespace after
// the run it consumed — its answer is "everything from the first byte after the
// last consumed closer", so it stays a suffix of the input — which means a model
// that closes its block and emits a newline yields "\n", not "". An exact
// `answer == ""` test reads that as "the model answered" and publishes the reply
// as merely garbled, losing the one signal that says WHY the review was empty.
//
// The sibling above uses an unclosed opener with no trailing byte, so its stub
// guarantees the assertion rather than exercising a shape a provider sends.
func TestInvokeSlot_ThinkOnlyReplyWithTrailingNewline_SetsSuppressedSignal(t *testing.T) {
	for _, content := range []string{
		"<think>reasoning about the diff</think>\n",
		"<think>reasoning about the diff</think>\n\n  ",
	} {
		e := NewEngine(&stubCompleter{content: content})
		r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

		assert.Equal(t, StatusOK, r.Status, "content=%q", content)
		assert.Equal(t, 0, r.ParsedFindingCount(), "content=%q", content)
		require.True(t, r.UnparseableResponse, "content=%q: still the generic unparseable bit", content)
		assert.True(t, r.ThinkSuppressed,
			"content=%q: the whole reply was a think run — trailing whitespace must not hide the signal", content)
	}
}

// TestInvokeSlot_GarbledReply_DoesNotSetThinkSuppressed is the complement: a
// reply with no think markup that parses to nothing keeps the generic bit only.
func TestInvokeSlot_GarbledReply_DoesNotSetThinkSuppressed(t *testing.T) {
	e := NewEngine(&stubCompleter{content: "verdict pending"})
	r := e.invokeSlot(context.Background(), Slot{Primary: Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}}})

	assert.Equal(t, StatusOK, r.Status)
	assert.True(t, r.UnparseableResponse)
	assert.False(t, r.ThinkSuppressed, "no think markup — not a think suppression")
}

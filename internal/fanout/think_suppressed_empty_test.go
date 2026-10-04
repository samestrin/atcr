package fanout

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// thinkSuppressedContent's empty-content guard was uncovered AND survived mutation:
// deleting it left the suite green, because SplitThink("") returns ("","") and the
// length test then reads 0 < 0 = false, so the function already answered false.
//
// The equivalence is real but it is exactly what no test stated, and the predicate
// now has two callers that both depend on it — the cache gate must not refuse an
// empty reply on think-suppression grounds, and invokeSlot must not record one as
// think-suppressed. Pinning the equivalence is what stops a later edit to SplitThink
// or to the length test from silently reclassifying every empty reply
// (TD internal/fanout/engine.go:1309).
func TestThinkSuppressedContent(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		content string
		want    bool
	}{
		// The guarded input. An empty reply means "the provider sent nothing", which
		// has a different remedy from "the model spent the reply thinking".
		"empty": {"", false},
		// Whitespace-only carries no think markup either: the length test is what
		// rules it out, since SplitThink returns it unchanged.
		"whitespace only": {"   \n\t ", false},
		// The shape the predicate exists for: the strip removed a leading run and
		// what remains is blank.
		"wholly a leading think run":      {"\x3cthink\x3ereasoning only\x3c/think\x3e", true},
		"leading run with trailing blank": {"\x3cthink\x3ereasoning\x3c/think\x3e\n", true},
		// A real answer after the run is not suppressed — the reply said something.
		"leading run then an answer": {"\x3cthink\x3ereasoning\x3c/think\x3e NO FINDINGS", false},
		"no markup at all":           {"NO FINDINGS", false},
	} {
		assert.Equal(t, tc.want, thinkSuppressedContent(tc.content), name)
	}
}

// The cache-gate half of the contract, stated directly: an empty reply must stay
// CACHEABLE as far as this predicate is concerned. Refusing it here would conflate
// "nothing arrived" with "the model thought instead of answering", and the gate has
// other, sharper reasons to decline an empty reply if it should.
func TestThinkSuppressedContent_EmptyReplyIsNotAThinkSuppressionGroundToRefuseACache(t *testing.T) {
	t.Parallel()
	assert.False(t, thinkSuppressedContent(""),
		"the diff-cache gate reads this predicate; an empty reply must not be declined on think-suppression grounds")
}

// The predicate must have exactly ONE definition. InvokeSlot's truncated-failover
// arm carried its own inline copy of the same length+TrimSpace test while the
// function's doc claimed "two callers, one predicate" — so a later edit to
// thinkSuppressedContent would leave that arm testing the OLD rule, and the two
// would disagree about which replies count as think-only without any test noticing
// (TD internal/fanout/engine.go:1018).
//
// A drift test, not a behavioural one: the behaviour is already covered, and what
// needs pinning is the STRUCTURAL rule that the inline copy was removed. Read the
// source rather than asserting on a call — a re-inlined copy would still ship the
// call elsewhere and pass a call-count check.
func TestThinkSuppressedContent_HasNoInlineCopyInInvokeSlot(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("engine.go")
	require.NoError(t, err)
	// The inline copy's tell: a SplitThink call whose result feeds a hand-written
	// length + TrimSpace test. thinkSuppressedContent's own body is the only place
	// that shape may now appear.
	body := string(src)
	inline := regexp.MustCompile(`(?m)if answer, _ := llmclient\.SplitThink\(`)
	assert.NotRegexp(t, inline, body,
		"invokeSlot must call thinkSuppressedContent rather than carrying its own copy of the predicate — "+
			"an inline copy drifts silently from the hoisted one it claims to share")
	assert.Contains(t, body, "if thinkSuppressedContent(r.Content) {",
		"the truncated-failover arm must route through the shared predicate")
}

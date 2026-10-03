package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

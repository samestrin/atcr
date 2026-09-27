package fanout

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guard chunker.go mergeResultGroup: the parsed-finding memo + UnparseableChunks
// reset copied from g[0]. The comment claims g[0] "never carries" these in
// practice; this pin crafts exactly that impossible state and asserts the merge
// still recomputes from the chunks rather than leaking chunk 0's stale values —
// so deleting the reset fails here instead of silently corrupting a future
// caller that does populate them.
func TestMergeResultGroup_StaleChunkZeroMemoDoesNotLeak(t *testing.T) {
	g0 := Result{Agent: "p", Model: "m1", Status: StatusOK, Content: "chunk zero"}
	g0.UnparseableChunks = 7 // stale: g[0] never really carries this
	g0.parsedFindingCount = 3
	g0.parsedFindingCountSet = true
	g1 := Result{Agent: "p", Model: "m1", Status: StatusOK, Content: "chunk one"}

	out := mergeResultGroup([]Result{g0, g1}, nil)
	require.NotNil(t, &out)
	assert.Equal(t, 0, out.UnparseableChunks,
		"neither chunk is unparseable; chunk 0's stale count must not leak into the merge")
	assert.False(t, out.UnparseableResponse)
}

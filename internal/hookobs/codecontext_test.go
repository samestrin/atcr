package hookobs

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
)

// TestWrap_RecordsCodeContext covers the field an audit consumer uses to answer
// "which files went to the model on this call". Only the fan-out engine knows
// it, so it travels on the context like the agent name does; if it did not
// reach the observation, the consumer would be left re-parsing the prompt.
func TestWrap_RecordsCodeContext(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	refs := []CodeRef{
		{Path: "alpha.go", Body: "diff --git a/alpha.go b/alpha.go\n"},
		{Path: "beta.go", Body: "diff --git a/beta.go b/beta.go\n"},
	}
	ctx := WithCall(observedCtx(obs, &bytes.Buffer{}), Call{AgentName: "security-reviewer", CodeContext: refs})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Equal(t, refs, obs.calls()[0].CodeContext)
}

// TestWrap_NoCodeContextStaysNil: a call made outside a review (verify, debate,
// benchmark) has no file payload to report, and must report nil rather than an
// empty slice a consumer would serialize as [].
func TestWrap_NoCodeContextStaysNil(t *testing.T) {
	srv := chatServer(t, http.StatusOK, okCompletion)
	inv := testInvocation(t, srv)
	obs := &recordingObserver{}
	ctx := WithCall(observedCtx(obs, &bytes.Buffer{}), Call{Stage: "verify"})

	_, err := Wrap(ctx, llmclient.New()).Complete(ctx, inv)
	require.NoError(t, err)

	require.Len(t, obs.calls(), 1)
	assert.Nil(t, obs.calls()[0].CodeContext)
}

// TestWithCall_CodeContextMerge: the engine is the only layer that sets it, but
// the merge rules still have to hold — an outer layer contributing nothing must
// not erase it, and a chunked agent's own slice must win over anything already
// present.
func TestWithCall_CodeContextMerge(t *testing.T) {
	agentRefs := []CodeRef{{Path: "alpha.go", Body: "a"}}
	chunkRefs := []CodeRef{{Path: "beta.go", Body: "b"}}

	t.Run("an empty contribution does not erase", func(t *testing.T) {
		ctx := WithCall(context.Background(), Call{AgentName: "agent-a", CodeContext: agentRefs})
		ctx = WithCall(ctx, Call{Stage: "verify"})

		assert.Equal(t, agentRefs, CallFrom(ctx).CodeContext)
	})

	t.Run("innermost wins", func(t *testing.T) {
		ctx := WithCall(context.Background(), Call{CodeContext: agentRefs})
		ctx = WithCall(ctx, Call{CodeContext: chunkRefs})

		assert.Equal(t, chunkRefs, CallFrom(ctx).CodeContext)
	})
}

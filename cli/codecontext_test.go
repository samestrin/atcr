package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/hookobs"
)

// TestAdaptCodeRefs_DoesNotAliasEngineState is the reason adaptCodeRefs
// allocates instead of handing the internal slice straight out. The engine
// builds one slice per agent and reuses it for every invocation that agent
// makes — and buildFallbackAgent shares the primary's — so returning it
// directly would let an observer's write to one record reach another
// invocation's. An observer must not be able to alter what it observes.
func TestAdaptCodeRefs_DoesNotAliasEngineState(t *testing.T) {
	engineOwned := []hookobs.CodeRef{{Path: "alpha.go", Body: "diff"}}

	got := adaptCodeRefs(engineOwned)
	require.Len(t, got, 1)
	got[0].Path = "clobbered"

	assert.Equal(t, "alpha.go", engineOwned[0].Path,
		"the exported slice must not share backing storage with the engine's")
}

// TestAdaptCodeRefs_EmptyStaysNil keeps a record for a call with no file
// payload free of an empty slice that would serialize as [] rather than being
// omitted.
func TestAdaptCodeRefs_EmptyStaysNil(t *testing.T) {
	assert.Nil(t, adaptCodeRefs(nil))
	assert.Nil(t, adaptCodeRefs([]hookobs.CodeRef{}))
}

// TestObserverAdapter_PreservesUnattributedCodeRef: a section whose path could
// not be determined still reaches the consumer with its body intact. Dropping
// it would silently remove content the model was actually sent from the audit
// record — the one outcome an audit trail cannot have.
func TestObserverAdapter_PreservesUnattributedCodeRef(t *testing.T) {
	obs := &recordingObserver{}

	observerAdapter{observer: obs}.OnModelInvocation(context.Background(), hookobs.Invocation{
		Model:       "m",
		CodeContext: []hookobs.CodeRef{{Path: "", Body: "diff --cc merged.go\n"}},
	})

	require.Len(t, obs.got, 1)
	require.Len(t, obs.got[0].CodeContext, 1)
	assert.Empty(t, obs.got[0].CodeContext[0].Path)
	assert.Equal(t, "diff --cc merged.go\n", obs.got[0].CodeContext[0].Body)
}

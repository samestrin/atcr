package fanout

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
)

// metaTruncatingCompleter implements MetaCompleter so the single-shot path can
// observe a finish_reason=length truncation on the returned Result. It also
// satisfies Completer (Complete) for the degrade path.
type metaTruncatingCompleter struct {
	content   string
	truncated bool
}

func (m *metaTruncatingCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return m.content, nil
}

func (m *metaTruncatingCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{Content: m.content, Truncated: m.truncated}, nil
}

// --- Task 1: the truncation signal reaches the Result on both paths ----------

func TestSingleShot_SetsResponseTruncated(t *testing.T) {
	e := NewEngine(&metaTruncatingCompleter{content: "ramble, no findings", truncated: true})
	r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}})
	assert.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ResponseTruncated, "single-shot must surface finish_reason=length onto the Result")
	assert.Equal(t, "ramble, no findings", r.Content)
}

func TestSingleShot_NoTruncationWhenClean(t *testing.T) {
	e := NewEngine(&metaTruncatingCompleter{content: "HIGH|a.go:1|b|f|correctness|5|e|bruce", truncated: false})
	r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "m"}})
	assert.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ResponseTruncated)
}

// A completer implementing only UsageCompleter (no MetaCompleter) still works —
// ResponseTruncated stays false (graceful degradation, no signal available).
func TestSingleShot_UsageOnlyCompleterLeavesTruncationFalse(t *testing.T) {
	e := NewEngine(&usageCompleter{})
	r := e.invokeAgent(context.Background(), Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "claude"}})
	assert.Equal(t, StatusOK, r.Status)
	assert.False(t, r.ResponseTruncated)
}

func TestToolLoop_SetsResponseTruncatedOnFinalTurn(t *testing.T) {
	// A tool-capable agent whose FINAL content-bearing turn hit finish_reason=length.
	sc := &scriptedChat{turns: []chatTurn{{content: "ramble", truncated: true}}}
	e := NewEngine(sc, WithDispatcher(&fakeDispatcher{}))
	r := e.invokeAgent(context.Background(), Agent{
		Name: "ronin", Tools: true, SupportsFC: true,
		Invocation: llmclient.Invocation{Model: "m"},
	})
	assert.Equal(t, StatusOK, r.Status)
	assert.True(t, r.ResponseTruncated, "tool loop must surface a truncated final answer onto the Result")
}

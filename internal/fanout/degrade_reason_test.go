package fanout

import (
	"context"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A degraded agent must record WHY it fell back, not just THAT it did.
// tools_degraded alone forces a reader to correlate status.json, the registry,
// and a live doctor run to recover the cause — and the rate-limit evidence that
// last input depends on decays, so a past degradation eventually cannot be
// explained at all.

// dispatchAgent is the single site that decides to degrade, and it can
// distinguish all three of its causes. An agent whose model is not declared
// function-calling capable never reaches the harness check.
func TestDispatchAgent_RecordsModelNotCapableReason(t *testing.T) {
	e := NewEngine(&chatStubCompleter{}, WithDispatcher(newFakeDispatcher()))
	r := e.dispatchAgent(context.Background(), Agent{Name: "a", Tools: true, SupportsFC: false})
	require.True(t, r.ToolsDegraded, "a tools:true agent on a non-FC model must degrade")
	assert.Equal(t, degradeReasonModelNotCapable, r.ToolsDegradedReason)
}

// An FC-capable model whose completer cannot chat is a wiring failure, not a
// registry one — the two must not collapse to the same recorded cause.
func TestDispatchAgent_RecordsNoChatCompleterReason(t *testing.T) {
	e := NewEngine(&plainStubCompleter{}, WithDispatcher(newFakeDispatcher()))
	r := e.dispatchAgent(context.Background(), Agent{Name: "a", Tools: true, SupportsFC: true})
	require.True(t, r.ToolsDegraded)
	assert.Equal(t, degradeReasonNoChatCompleter, r.ToolsDegradedReason)
}

// A capable model and a capable completer with no harness wired is the third
// distinct cause.
func TestDispatchAgent_RecordsNoDispatcherReason(t *testing.T) {
	e := NewEngine(&chatStubCompleter{})
	r := e.dispatchAgent(context.Background(), Agent{Name: "a", Tools: true, SupportsFC: true})
	require.True(t, r.ToolsDegraded)
	assert.Equal(t, degradeReasonNoDispatcher, r.ToolsDegradedReason)
}

// A non-degrading path must leave the reason empty, so the field is never
// evidence of a degradation that did not happen.
func TestDispatchAgent_NoReasonWhenNotDegraded(t *testing.T) {
	e := NewEngine(&chatStubCompleter{}, WithDispatcher(newFakeDispatcher()))
	r := e.dispatchAgent(context.Background(), Agent{Name: "a", Tools: false})
	require.False(t, r.ToolsDegraded)
	assert.Empty(t, r.ToolsDegradedReason)
}

// The headline case from the 2026-08-10 review: a rate-limited primary fails
// over to a backup that lacks function calling. That is a registry-shaped
// problem distinct from a primary that was never capable, so invokeSlot
// promotes the reason once it knows the attempt was a fallback.
func TestInvokeSlot_PromotesReasonToFallbackNotCapable(t *testing.T) {
	e := NewEngine(&failFirstChatCompleter{}, WithDispatcher(newFakeDispatcher()))
	r := e.invokeSlot(context.Background(), Slot{
		Primary:   Agent{Name: "primary", Tools: true, SupportsFC: true},
		Fallbacks: []Agent{{Name: "backup", Tools: true, SupportsFC: false}},
	})
	require.True(t, r.FallbackUsed, "the primary must have failed over")
	require.True(t, r.ToolsDegraded)
	assert.Equal(t, degradeReasonFallbackNotCapable, r.ToolsDegradedReason,
		"an incapable BACKUP is a different diagnosis from an incapable primary")
}

// The manifest carries the per-agent cause, keyed by agent name, so the two
// artifacts agree without the reader correlating them.
func TestReviewStageFor_CarriesDegradationReasons(t *testing.T) {
	rs := reviewStageFor([]Result{
		{Agent: "clean", ToolsRequested: true, Tools: true},
		{Agent: "bruce", ToolsRequested: true, Tools: true, ToolsDegraded: true, ToolsDegradedReason: degradeReasonFallbackNotCapable},
		{Agent: "dax", ToolsRequested: true, Tools: true, ToolsDegraded: true, ToolsDegradedReason: degradeReasonNoChatCompleter},
	})
	require.NotNil(t, rs)
	assert.Equal(t, map[string]string{
		"bruce": degradeReasonFallbackNotCapable,
		"dax":   degradeReasonNoChatCompleter,
	}, rs.ToolsDegradedReason)
	assert.NotContains(t, rs.ToolsDegradedReason, "clean",
		"a non-degraded agent must not appear in the reason map")
}

// A run with no degradation must leave the map nil so the manifest stays
// byte-identical to a pre-field one.
func TestReviewStageFor_NoReasonMapWhenNothingDegraded(t *testing.T) {
	rs := reviewStageFor([]Result{{Agent: "a", ToolsRequested: true, Tools: true}})
	require.NotNil(t, rs)
	assert.Nil(t, rs.ToolsDegradedReason)
}

// status.json mirrors the reason for the same agent (the TD's "mirror the same
// field on the per-agent status.json so the two artifacts agree").
func TestStatusFor_MirrorsDegradationReason(t *testing.T) {
	st := statusFor(Result{
		Agent: "bruce", Tools: true, ToolsRequested: true,
		ToolsDegraded: true, ToolsDegradedReason: degradeReasonFallbackNotCapable,
	}, findingsResult{})
	assert.Equal(t, degradeReasonFallbackNotCapable, st.ToolsDegradedReason)
}

// The resume path rebuilds the manifest from statuses, so it must reproduce the
// same map rather than dropping the cause on a resumed run.
func TestReviewStageFromStatuses_CarriesDegradationReasons(t *testing.T) {
	rs := reviewStageFromStatuses([]AgentStatus{
		{Agent: "bruce", ToolsRequested: true, ToolsDegraded: true, ToolsDegradedReason: degradeReasonFallbackNotCapable},
	})
	require.NotNil(t, rs)
	assert.Equal(t, map[string]string{"bruce": degradeReasonFallbackNotCapable}, rs.ToolsDegradedReason)
}

// --- stubs -------------------------------------------------------------

// plainStubCompleter implements only Completer — not ChatCompleter — so a
// tool-enabled agent degrades for want of a chat-capable client.
type plainStubCompleter struct{}

func (c *plainStubCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	return "no findings", nil
}

// chatStubCompleter additionally implements ChatCompleter, so the harness is
// available whenever a dispatcher is wired.
type chatStubCompleter struct{}

func (c *chatStubCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	return "no findings", nil
}

func (c *chatStubCompleter) Chat(ctx context.Context, inv llmclient.Invocation, msgs []llmclient.Message, defs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	return &llmclient.ChatResponse{Message: llmclient.Message{Content: strPtr("no findings")}}, nil
}

// failFirstChatCompleter fails the first Complete call (the primary) and
// succeeds afterward, driving the fallback chain exactly once.
type failFirstChatCompleter struct{ calls int }

func (c *failFirstChatCompleter) Complete(ctx context.Context, inv llmclient.Invocation) (string, error) {
	c.calls++
	if c.calls == 1 {
		return "", assertRateLimited{}
	}
	return "no findings", nil
}

func (c *failFirstChatCompleter) Chat(ctx context.Context, inv llmclient.Invocation, msgs []llmclient.Message, defs []llmclient.ToolDef) (*llmclient.ChatResponse, error) {
	c.calls++
	if c.calls == 1 {
		return nil, assertRateLimited{}
	}
	return &llmclient.ChatResponse{Message: llmclient.Message{Content: strPtr("no findings")}}, nil
}

func strPtr(s string) *string { return &s }

type assertRateLimited struct{}

func (assertRateLimited) Error() string { return "429 rate limited" }

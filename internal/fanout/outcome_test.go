package fanout

import (
	"context"
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// slotWithFallback builds a non-serial slot whose primary fails over to the
// named fallback agents (each its own model).
func slotWithFallback(primary string, fallbacks ...string) Slot {
	s := Slot{Primary: Agent{Name: primary, Invocation: llmclient.Invocation{Model: primary}, PayloadMode: "blocks"}}
	for _, fb := range fallbacks {
		s.Fallbacks = append(s.Fallbacks, Agent{Name: fb, Invocation: llmclient.Invocation{Model: fb}, PayloadMode: "diff"})
	}
	return s
}

func TestRun_PrimaryFailsFallbackSucceeds(t *testing.T) {
	f := newFake()
	f.failFor["p"] = errors.New("connection refused") // primary fails
	e := NewEngine(f)

	results := e.Run(context.Background(), []Slot{slotWithFallback("p", "fb")})
	require.Len(t, results, 1)
	r := results[0]

	assert.Equal(t, StatusOK, r.Status, "fallback should rescue the slot")
	assert.Equal(t, "p", r.Agent, "attribution stays with the primary/slot name")
	assert.True(t, r.FallbackUsed)
	assert.Equal(t, "p", r.FallbackFrom)
	assert.Equal(t, "review by fb", r.Content)
	assert.Equal(t, 1, f.callCount("p"))
	assert.Equal(t, 1, f.callCount("fb"))
}

func TestRun_FallbackChainExhaustedMarksFailed(t *testing.T) {
	f := newFake()
	f.failFor["p"] = errors.New("connection refused")
	f.failFor["fb1"] = errors.New("timeout")
	f.failFor["fb2"] = errors.New("503")
	e := NewEngine(f)

	results := e.Run(context.Background(), []Slot{slotWithFallback("p", "fb1", "fb2")})
	require.Len(t, results, 1)
	r := results[0]

	assert.Equal(t, StatusFailed, r.Status)
	assert.Equal(t, "p", r.Agent)
	assert.Equal(t, "blocks", r.PayloadMode, "failed slot records the primary's payload provenance")
	// Every link in the chain was attempted.
	assert.Equal(t, 1, f.callCount("p"))
	assert.Equal(t, 1, f.callCount("fb1"))
	assert.Equal(t, 1, f.callCount("fb2"))
}

func TestRun_FallbackNotTriedWhenPrimarySucceeds(t *testing.T) {
	f := newFake()
	e := NewEngine(f)

	results := e.Run(context.Background(), []Slot{slotWithFallback("p", "fb")})
	require.Len(t, results, 1)
	assert.Equal(t, StatusOK, results[0].Status)
	assert.False(t, results[0].FallbackUsed)
	assert.Equal(t, 1, f.callCount("p"))
	assert.Equal(t, 0, f.callCount("fb"), "fallback must not run when primary succeeds")
}

func TestOutcome_PartialWhenSomeFail(t *testing.T) {
	results := []Result{
		{Agent: "a", Status: StatusOK},
		{Agent: "b", Status: StatusFailed, Err: errors.New("boom")},
		{Agent: "c", Status: StatusOK},
	}
	s, err := Outcome(results)
	require.NoError(t, err, "one failure among successes is not a run-level error")
	assert.True(t, s.Partial)
	assert.Equal(t, 3, s.Total)
	assert.Equal(t, 2, s.Succeeded)
	assert.Equal(t, 1, s.Failed)
}

func TestOutcome_NotPartialWhenAllSucceed(t *testing.T) {
	s, err := Outcome([]Result{{Status: StatusOK}, {Status: StatusOK}})
	require.NoError(t, err)
	assert.False(t, s.Partial)
}

func TestOutcome_AllFailIsError(t *testing.T) {
	results := []Result{
		{Agent: "reviewer-a", Status: StatusTimeout, Err: errors.New("timeout")},
		{Agent: "reviewer-b", Status: StatusFailed, Err: errors.New("connection refused")},
	}
	s, err := Outcome(results)
	require.Error(t, err)
	assert.False(t, s.Partial, "partial is false when nothing succeeded")
	assert.Equal(t, 0, s.Succeeded)
	// Deterministic, sorted, lists each agent and its reason.
	assert.Equal(t, "all agents failed: reviewer-a (timeout), reviewer-b (connection refused)", err.Error())
}

func TestOutcome_EmptyResultsIsError(t *testing.T) {
	s, err := Outcome(nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyRoster)
	assert.Equal(t, 0, s.Total)
}

func TestOutcome_AllFailUsesSentinel(t *testing.T) {
	_, err := Outcome([]Result{{Agent: "a", Status: StatusFailed, Err: errors.New("boom")}})
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAllAgentsFailed)
}

func TestOutcome_EmptyAgentNameRendersPlaceholder(t *testing.T) {
	_, err := Outcome([]Result{{Agent: "", Status: StatusFailed, Err: errors.New("boom")}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "<unnamed> (boom)")
}

// A successful result carrying a stray Err must never appear in the all-failed
// list — formatFailures filters to non-OK rows independent of its caller.
func TestOutcome_FormatFailuresSkipsOKRows(t *testing.T) {
	out := formatFailures([]Result{
		{Agent: "ok", Status: StatusOK, Err: errors.New("should be ignored")},
		{Agent: "bad", Status: StatusFailed, Err: errors.New("real")},
	})
	assert.Equal(t, "bad (real)", out)
}

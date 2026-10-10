package fanout

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
)

// Every failover names its reason in exactly one line (Epic 35.16.11.2.2.7 T2).
// A provider error used to hand the slot to the backup with no line at all; the
// truncation and empty-reply demotions already log their own line and must not
// gain a second. No line is logged when no failover follows: the last agent in
// the chain, or a context already done when the attempt returns.
func TestInvokeSlot_FailoverReasonLine(t *testing.T) {
	const failoverMsg = `msg="reviewer attempt failed; failing over"`
	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "primary"}},
		Fallbacks: []Agent{{Name: "bruce-fb", Invocation: llmclient.Invocation{Model: "fallback"}}},
	}

	t.Run("provider error rescued by the fallback: one line naming primary, model and error", func(t *testing.T) {
		f := newFake()
		f.failFor["primary"] = errors.New("HTTP 503: upstream overloaded")
		ctx, buf := failoverLogCapture()
		r := NewEngine(f, WithTruncationFailover()).invokeSlot(ctx, slot)

		require.Equal(t, StatusOK, r.Status, "the fallback rescued the slot")
		assert.True(t, r.FallbackUsed)
		assert.Equal(t,
			`level=WARN `+failoverMsg+` agent=bruce model=primary err="HTTP 503: upstream overloaded"`+"\n",
			buf.String())
	})

	t.Run("truncated zero findings: still exactly one line", func(t *testing.T) {
		c := &countingMetaCompleter{byModel: map[string]llmclient.Completion{
			"primary":  {Content: "I was thinking hard but never emitted a finding", Truncated: true},
			"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
		}}
		ctx, buf := failoverLogCapture()
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.True(t, r.FallbackUsed)
		assert.Zero(t, strings.Count(buf.String(), failoverMsg), buf.String())
		assert.Equal(t,
			`level=WARN msg="reviewer response truncated with zero findings; failing over" agent=bruce model=primary`+"\n",
			buf.String())
	})

	t.Run("empty reply: still exactly one line", func(t *testing.T) {
		c := &countingMetaCompleter{byModel: map[string]llmclient.Completion{
			"primary":  {Content: ""},
			"fallback": {Content: "HIGH|a.go:1|bug|fix|correctness|5|ev|bruce"},
		}}
		ctx, buf := failoverLogCapture()
		r := NewEngine(c, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.True(t, r.FallbackUsed)
		assert.Equal(t,
			`level=WARN msg="reviewer returned an empty response; failing over" agent=bruce model=primary`+"\n",
			buf.String())
	})

	t.Run("failed last agent: no failover line", func(t *testing.T) {
		f := newFake()
		f.failFor["primary"] = errors.New("boom")
		f.failFor["fallback"] = errors.New("HTTP 500: backup down")
		ctx, buf := failoverLogCapture()
		r := NewEngine(f, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.Equal(t, StatusFailed, r.Status)
		assert.Equal(t, 1, strings.Count(buf.String(), failoverMsg), buf.String())
		assert.Equal(t,
			`level=WARN `+failoverMsg+` agent=bruce model=primary err=boom`+"\n",
			buf.String(), "only the primary's failure is followed by a failover")
	})

	t.Run("single agent failing: no failover line", func(t *testing.T) {
		f := newFake()
		f.failFor["primary"] = errors.New("boom")
		ctx, buf := failoverLogCapture()
		r := NewEngine(f, WithTruncationFailover()).invokeSlot(ctx, Slot{Primary: slot.Primary})

		assert.Equal(t, StatusFailed, r.Status)
		assert.Empty(t, buf.String())
	})

	t.Run("primary fails with the context already cancelled: no failover line", func(t *testing.T) {
		logCtx, buf := failoverLogCapture()
		ctx, cancel := context.WithCancel(logCtx)
		defer cancel()
		f := newFake()
		f.failFor["primary"] = errors.New("HTTP 401 unauthorized")
		f.onStart = func(model string) {
			if model == "primary" {
				cancel()
			}
		}
		r := NewEngine(f, WithTruncationFailover()).invokeSlot(ctx, slot)

		assert.NotEqual(t, StatusOK, r.Status)
		assert.Zero(t, f.callCount("fallback"), "precondition: the walk stopped on the done context")
		assert.Empty(t, buf.String())
	})
}

package fanout

// TD internal/fanout/engine.go:602 (sprint 35.16.11.2.2.4) — the think strip turns a whole
// class of inline-thinking replies into zero-findings replies, and zero findings is the
// truncation-failover trigger, so an endpoint that reasons inline walks the ENTIRE fallback
// chain — one full provider call per chain member — and the last member still records
// unparseable_response. The sprint pinned the failover cost as an accepted loss (sprint-plan
// §4.2); the FIX's stated minimum is: "At minimum record the chain-walk cost so an operator
// sees N backup calls bought zero findings."
//
// RED: after a chain exhausts on replies that were wholly a think run, a warn log must name
// the attempt count and the think-only cause, so the wasted spend is visible in the run log
// instead of discoverable only by diffing status.json.

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/log"
)

// thinkOnlyCompleter returns the same TRUNCATED think-only reply for every
// chain member — the waste shape: truncation demotes the reply, the chain
// descends, and every member fails identically because it reasons inline.
type thinkOnlyCompleter struct{ content string }

func (s *thinkOnlyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return s.content, nil
}

func (s *thinkOnlyCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	return llmclient.Completion{Content: s.content, Truncated: true}, nil
}

func TestInvokeSlot_ThinkOnlyChain_LogsTheWastedWalk(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&thinkOnlyCompleter{content: "<think>\ncareful reasoning, no findings ever"},
		WithLogger(logger), WithTruncationFailover())

	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-primary"}},
		Fallbacks: []Agent{{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-a"}}, {Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-b"}}},
	}
	// The walk's warn lines go to the CONTEXT logger (log.FromContext), which
	// ExecuteReview seeds — WithLogger alone is the invokeAgent-scoped path.
	ctx := log.NewContext(context.Background(), logger)
	r := e.invokeSlot(ctx, slot)

	require.ErrorIs(t, r.Err, errTruncatedZeroFindings,
		"a truncated think-only reply demotes to the truncated-zero-findings failure")
	out := buf.String()
	require.Contains(t, out, "think-only",
		"the chain-walk cost log must name the think-only cause")
	require.Contains(t, out, "think_only_attempts=3",
		"the log must state that all 3 chain members were think-only")
}

// TestInvokeSlot_ThinkOnlyChain_CountsTheClosedTrailingNewlineShape pins the
// ROUTINE input the test above cannot reach. SplitThink keeps the whitespace
// after the run it consumed, so a model that CLOSES its block and emits a
// newline yields "\n". An exact `answer == ""` test reads that as an answer and
// the chain-walk warn never fires — on the commonest inline-reasoning shape
// there is, which is the spend this log exists to make visible.
func TestInvokeSlot_ThinkOnlyChain_CountsTheClosedTrailingNewlineShape(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&thinkOnlyCompleter{content: "<think>careful reasoning, no findings ever</think>\n"},
		WithLogger(logger), WithTruncationFailover())

	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-primary"}},
		Fallbacks: []Agent{{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-a"}}, {Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-b"}}},
	}
	ctx := log.NewContext(context.Background(), logger)
	r := e.invokeSlot(ctx, slot)

	require.ErrorIs(t, r.Err, errTruncatedZeroFindings)
	require.Contains(t, buf.String(), "think_only_attempts=3",
		"a closed think block followed by a newline is still a wholly-reasoning reply")
}

// TestInvokeSlot_EmptyContentChain_IsNotCountedAsThinkOnly is the complement and
// the second half of the same defect: SplitThink("") returns ("", ""), so an
// empty-content reply satisfied the old `answer == ""` test and was reported as
// wholly-reasoning. "The provider returned nothing" and "the model spent the whole
// reply thinking" have opposite remedies, so they must not share a counter.
func TestInvokeSlot_EmptyContentChain_IsNotCountedAsThinkOnly(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&thinkOnlyCompleter{content: ""}, WithLogger(logger), WithTruncationFailover())

	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-primary"}},
		Fallbacks: []Agent{{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-a"}}},
	}
	ctx := log.NewContext(context.Background(), logger)
	e.invokeSlot(ctx, slot)

	require.NotContains(t, buf.String(), "think_only_attempts",
		"an empty reply carries no think markup — it must not be reported as wholly-reasoning")
}

// A NON-TRUNCATED think-only chain. The gate-1 increment for a StatusOK reply sits
// inside the `if r.Status == StatusOK { ... }` block, and that block unconditionally
// RETURNS — so every increment there is immediately followed by the walk ending, and
// the after-loop warning could never observe one. Only the truncated-failover
// increment could reach it, so a chain whose last attempt was an untruncated
// think-only reply (finish_reason=stop, the shape elsewhere called "the dangerous
// one") produced no warning at all however many attempts preceded it (TD
// internal/fanout/engine.go:1083).
type untruncatedThinkOnlyCompleter struct{ content string }

func (s *untruncatedThinkOnlyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return s.content, nil
}

func (s *untruncatedThinkOnlyCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	// Truncated stays FALSE: nothing demotes this reply, so the walk returns out of
	// the StatusOK block before the warning.
	return llmclient.Completion{Content: s.content}, nil
}

func TestInvokeSlot_ThinkOnlyChainWithANonTruncatedAnswerWarns(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&untruncatedThinkOnlyCompleter{content: "\x3cthink\x3ereasoning only, no answer\x3c/think\x3e"},
		WithLogger(logger))

	slot := Slot{Primary: Agent{Name: "archer", Invocation: llmclient.Invocation{Model: "m"}}}
	ctx := log.NewContext(context.Background(), logger)
	r := e.invokeSlot(ctx, slot)

	require.True(t, r.ThinkSuppressed,
		"precondition: the reply is wholly reasoning, so the think-only fact holds")
	out := buf.String()
	require.Contains(t, out, "think-only",
		"an untruncated think-only reply is the same wasted spend and must be warned about")
	require.Contains(t, out, "think_only_attempts=1",
		"and the count must survive the StatusOK early return to reach the warning")
}

// The merged record must SUM ThinkOnlyAttempts over its chunks, the way it sums
// UnparseableChunks: reading only g[0] would attribute one chunk's wasted spend to
// the whole persona and hide a later chunk's.
func TestMergeResultGroup_SumsThinkOnlyAttempts(t *testing.T) {
	merged := mergeResultGroup([]Result{
		{Agent: "greta", Status: StatusOK, Content: "c0", ThinkOnlyAttempts: 2},
		{Agent: "greta", Status: StatusOK, Content: "c1", ThinkOnlyAttempts: 1},
	}, nil)
	assert.Equal(t, 3, merged.ThinkOnlyAttempts,
		"the persona's think-only spend is the sum over its chunks")
}

// The StatusOK-block warning's guard is `thinkOnlyAttempts > 0 && r.ThinkSuppressed`,
// but the block that sets r.ThinkSuppressed increments thinkOnlyAttempts two lines
// later, so the first conjunct can never be false when the second is true — it is
// dead, and the comment's stated intent ("earlier attempts were too") cannot be
// expressed by it. So a single-shot agent with NO fallbacks and one think-suppressed
// reply printed "think-only replies exhausted the fallback chain", a false claim: no
// chain was walked and no backup was bought (TD internal/fanout/engine.go:1143).
func TestInvokeSlot_SingleAttemptThinkOnlyDoesNotClaimAChainWasExhausted(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&untruncatedThinkOnlyCompleter{content: "\x3cthink\x3ereasoning only, no answer\x3c/think\x3e"},
		WithLogger(logger))

	// One agent, no fallbacks: nothing to exhaust.
	slot := Slot{Primary: Agent{Name: "archer", Invocation: llmclient.Invocation{Model: "m"}}}
	ctx := log.NewContext(context.Background(), logger)
	r := e.invokeSlot(ctx, slot)
	require.True(t, r.ThinkSuppressed, "precondition: the reply is wholly reasoning")

	out := buf.String()
	require.Contains(t, out, "think-only",
		"the wasted spend is still worth warning about")
	assert.NotContains(t, out, "exhausted the fallback chain",
		"a single attempt exhausted no chain — the wording must describe what actually happened")
	assert.Contains(t, out, "zero findings",
		"the honest framing is that the walk bought zero findings")
}

// A completer whose FIRST call is a TRUNCATED think-only reply (so the chain
// descends and the truncated arm counts one attempt) and whose SUBSEQUENT calls are
// NON-truncated think-only replies (so the walk returns out of the StatusOK block).
// That is the only shape that drives the >1 arm of the StatusOK-block warning: at
// least one earlier attempt was think-only AND the final reply was too.
type truncatedThenCleanThinkOnlyCompleter struct{ calls int }

func (c *truncatedThenCleanThinkOnlyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	c.calls++
	if c.calls == 1 {
		return "\x3cthink\x3ereasoning only, no answer\x3c/think\x3e", nil
	}
	return "\x3cthink\x3ereasoning only, no answer\x3c/think\x3e", nil
}

func (c *truncatedThenCleanThinkOnlyCompleter) CompleteWithMeta(_ context.Context, _ llmclient.Invocation) (llmclient.Completion, error) {
	c.calls++
	return llmclient.Completion{Content: "\x3cthink\x3ereasoning only, no answer\x3c/think\x3e", Truncated: c.calls == 1}, nil
}

// The multi-attempt arm keeps the chain wording, and the warning must still say how
// many attempts were think-only, because that is the wasted spend an operator reads.
func TestInvokeSlot_MultiAttemptThinkOnlyKeepsTheChainWording(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&truncatedThenCleanThinkOnlyCompleter{}, WithLogger(logger), WithTruncationFailover())

	slot := Slot{
		Primary:   Agent{Name: "archer", Invocation: llmclient.Invocation{Model: "m"}},
		Fallbacks: []Agent{{Name: "archer", Invocation: llmclient.Invocation{Model: "b"}}},
	}
	ctx := log.NewContext(context.Background(), logger)
	e.invokeSlot(ctx, slot)

	out := buf.String()
	require.Contains(t, out, "exhausted the fallback chain",
		"an earlier attempt WAS think-only, so the chain-really-was-walked wording is honest here")
	require.Contains(t, out, "think_only_attempts=",
		"the wasted spend must be counted on the line")
}

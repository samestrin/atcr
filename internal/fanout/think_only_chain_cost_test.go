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

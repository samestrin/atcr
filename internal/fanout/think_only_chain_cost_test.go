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

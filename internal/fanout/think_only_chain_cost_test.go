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
)

// thinkOnlyCompleter returns the same think-only reply for every chain member.
type thinkOnlyCompleter struct{ content string }

func (s *thinkOnlyCompleter) Complete(_ context.Context, _ llmclient.Invocation) (string, error) {
	return s.content, nil
}

func TestInvokeSlot_ThinkOnlyChain_LogsTheWastedWalk(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	e := NewEngine(&thinkOnlyCompleter{content: "<think\ncareful reasoning, no findings ever"},
		WithLogger(logger), WithTruncationFailover())

	slot := Slot{
		Primary:   Agent{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-primary"}},
		Fallbacks: []Agent{{Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-a"}}, {Name: "bruce", Invocation: llmclient.Invocation{Model: "thinker-backup-b"}}},
	}
	r := e.invokeSlot(context.Background(), slot)

	require.True(t, r.UnparseableResponse, "the last member still records unparseable")
	out := buf.String()
	require.Contains(t, out, "think-only",
		"the chain-walk cost log must name the think-only cause")
	require.Contains(t, out, "3",
		"the log must state how many attempts were spent (3 chain members)")
}

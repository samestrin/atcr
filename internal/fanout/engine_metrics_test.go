package fanout

import (
	"context"
	"errors"
	"testing"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
)

// TestEngineRecordsAgentMetrics drives a roster of one success, one HTTP failure,
// and one timeout through Run and asserts the agent/API metrics are recorded.
// Not parallel: it asserts absolute counter values against the process-global
// DefaultRegistry, which it resets first (non-parallel tests never overlap the
// parallel ones, so the reset window is exclusive).
func TestEngineRecordsAgentMetrics(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	f := newFake()
	f.failFor["fail"] = &llmclient.HTTPStatusError{Status: 429}
	f.failFor["slow"] = context.DeadlineExceeded
	e := NewEngine(f)

	e.Run(context.Background(), []Slot{agentSlot("ok"), agentSlot("fail"), agentSlot("slow")})

	check := func(name string, want int64) {
		t.Helper()
		if got := metrics.Counter(name).Value(); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	check("atcr_agents_total", 3)
	check("atcr_agents_succeeded", 1)
	check("atcr_agents_failed", 1)
	check("atcr_agents_timed_out", 1)
	check("atcr_api_calls_total", 3)
	check(metrics.Key("atcr_api_errors_total", "status", "429"), 1)

	if got := metrics.Histogram("atcr_agent_duration_seconds").Count(); got != 3 {
		t.Errorf("atcr_agent_duration_seconds count = %d, want 3", got)
	}
}

// TestRecordAgentOutcome covers every branch of the outcome classifier directly,
// including the non-HTTP error path (no api_errors series) and the tool-call
// counter, which the single-shot Run path above cannot exercise.
func TestRecordAgentOutcome(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	recordAgentOutcome(Result{Status: StatusOK})                                                   // 1 API call (Turns 0)
	recordAgentOutcome(Result{Status: StatusFailed, Err: &llmclient.HTTPStatusError{Status: 500}}) // 1 API call
	recordAgentOutcome(Result{Status: StatusTimeout, ToolCalls: 4, Turns: 3})                      // 3 API calls (tool loop)
	recordAgentOutcome(Result{Status: StatusFailed, Err: errors.New("non-http failure")})          // 1 API call

	check := func(name string, want int64) {
		t.Helper()
		if got := metrics.Counter(name).Value(); got != want {
			t.Errorf("%s = %d, want %d", name, got, want)
		}
	}
	check("atcr_agents_succeeded", 1)
	check("atcr_agents_failed", 2)
	check("atcr_agents_timed_out", 1)
	check("atcr_api_calls_total", 6) // 1 + 1 + 3 (tool loop) + 1
	check(metrics.Key("atcr_api_errors_total", "status", "500"), 1)
	check("atcr_tool_calls_total", 4)
}

// TestRecordAgentOutcomeContextCancelledBeforeRequest verifies that a single-shot
// agent whose context was cancelled before any HTTP request does not inflate
// atcr_api_calls_total.
func TestRecordAgentOutcomeContextCancelledBeforeRequest(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	// context.DeadlineExceeded: cancelled before first HTTP round-trip, Turns==0
	recordAgentOutcome(Result{Status: StatusTimeout, Err: context.DeadlineExceeded})
	// context.Canceled: same scenario via SIGINT path
	recordAgentOutcome(Result{Status: StatusTimeout, Err: context.Canceled})

	if got := metrics.Counter("atcr_api_calls_total").Value(); got != 0 {
		t.Errorf("atcr_api_calls_total = %d, want 0 (context errors before any HTTP call must not count)", got)
	}
}

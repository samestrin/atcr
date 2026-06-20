package fanout

import (
	"context"
	"errors"
	"testing"
	"time"

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
	check("atcr_api_calls_total", 2) // slow agent's context.DeadlineExceeded before any HTTP call now correctly counts 0
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

// TestRecordAgentOutcomeNegativeTurns verifies a corrupt Result with negative
// Turns does not decrement atcr_api_calls_total; it should be treated as a
// single-shot (1 call) per the documented max(1,Turns) semantics.
func TestRecordAgentOutcomeNegativeTurns(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	recordAgentOutcome(Result{Status: StatusFailed, Turns: -1})

	if got := metrics.Counter("atcr_api_calls_total").Value(); got != 1 {
		t.Errorf("atcr_api_calls_total = %d, want 1 (negative Turns must be treated as single-shot)", got)
	}
}

// TestRecordAgentOutcomeZeroHTTPStatus verifies that an HTTPStatusError with
// Status==0 does not emit an uninformative {status="0"} error bucket.
func TestRecordAgentOutcomeZeroHTTPStatus(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	recordAgentOutcome(Result{Status: StatusFailed, Err: &llmclient.HTTPStatusError{Status: 0}})

	if got := metrics.Counter(metrics.Key("atcr_api_errors_total", "status", "0")).Value(); got != 0 {
		t.Errorf("atcr_api_errors_total{status=0} = %d, want 0 (non-positive status must not be recorded)", got)
	}
}

// TestRecordAgentOutcomeContextCancelledBeforeRequest verifies, with per-record
// semantics (Epic 4.11), that an agent which never completed an HTTP round-trip
// does not inflate atcr_api_calls_total — covering both the per-record path (a
// cancel-before-send record that did not reach the wire) and the nil fallback
// (circuit-open fail-fast and a corrupt negative Turns, which must never
// decrement the monotonic counter). All cases must total 0 (AC2, AC5).
func TestRecordAgentOutcomeContextCancelledBeforeRequest(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	// Per-record path: cancel-before-send via a deadline — the attempt was entered
	// but the bytes were never written, so its one record did not reach the wire.
	recordAgentOutcome(Result{
		Status:      StatusTimeout,
		Err:         context.DeadlineExceeded,
		CallRecords: []llmclient.CallRecord{{ReachedWire: false, Duration: time.Millisecond}},
	})
	// Per-record path: same scenario via the SIGINT (context.Canceled) path.
	recordAgentOutcome(Result{
		Status:      StatusTimeout,
		Err:         context.Canceled,
		CallRecords: []llmclient.CallRecord{{ReachedWire: false}},
	})
	// Nil fallback: circuit-open fail-fast makes no request and surfaces nil records.
	recordAgentOutcome(Result{Status: StatusFailed, Err: &llmclient.CircuitOpenError{Provider: "groq"}})
	// Nil fallback: a corrupt negative Turns + context error must not decrement.
	recordAgentOutcome(Result{Status: StatusTimeout, Err: context.DeadlineExceeded, Turns: -1})

	if got := metrics.Counter("atcr_api_calls_total").Value(); got != 0 {
		t.Errorf("atcr_api_calls_total = %d, want 0 (no completed HTTP round-trip must count or decrement)", got)
	}
	if got := metrics.Histogram(metrics.NameAPICallDurationSeconds).Count(); got != 0 {
		t.Errorf("%s count = %d, want 0 (no attempt reached the wire)", metrics.NameAPICallDurationSeconds, got)
	}
}

// TestRecordAgentOutcome_PerAttemptCountsAndTimes verifies the per-record path:
// atcr_api_calls_total counts CallRecords that reached the wire (retries
// included), and the duration histogram observes exactly one sample per wire
// record — so the histogram's count always equals the call count (AC1, AC3).
func TestRecordAgentOutcome_PerAttemptCountsAndTimes(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	// AC1: a single-shot whose context expired after one real round-trip has one
	// wire record and must count 1 (the Turns-based heuristic counts it 0 today).
	recordAgentOutcome(Result{
		Status: StatusTimeout,
		Err:    context.DeadlineExceeded,
		CallRecords: []llmclient.CallRecord{
			{ReachedWire: true, Duration: 100 * time.Millisecond},
		},
	})
	// Per-attempt counting: three wire attempts (two retries + a success) count 3.
	recordAgentOutcome(Result{
		Status: StatusOK,
		CallRecords: []llmclient.CallRecord{
			{ReachedWire: true, Duration: 10 * time.Millisecond},
			{ReachedWire: true, Duration: 20 * time.Millisecond},
			{ReachedWire: true, Duration: 30 * time.Millisecond},
		},
	})

	if got := metrics.Counter(metrics.NameAPICallsTotal).Value(); got != 4 {
		t.Errorf("atcr_api_calls_total = %d, want 4 (1 + 3 wire attempts)", got)
	}
	h := metrics.Histogram(metrics.NameAPICallDurationSeconds)
	if got := h.Count(); got != 4 {
		t.Errorf("%s count = %d, want 4 (one sample per wire record == call count)", metrics.NameAPICallDurationSeconds, got)
	}
	// 100 + 10 + 20 + 30 = 160ms total observed.
	if got := h.Sum(); got < 0.159 || got > 0.161 {
		t.Errorf("%s sum = %f, want ~0.160 seconds", metrics.NameAPICallDurationSeconds, got)
	}
}

// TestRecordAgentOutcome_NonWireRecordsCountZero verifies cancel-before-send (a
// record that never reached the wire) and circuit-open fail-fast (nil records +
// CircuitOpenError) both count zero API calls and emit no latency sample (AC2).
func TestRecordAgentOutcome_NonWireRecordsCountZero(t *testing.T) {
	metrics.DefaultRegistry.Reset()
	t.Cleanup(metrics.DefaultRegistry.Reset)

	// cancel-before-send: an attempt was entered but no bytes were written.
	recordAgentOutcome(Result{
		Status:      StatusTimeout,
		Err:         context.Canceled,
		CallRecords: []llmclient.CallRecord{{ReachedWire: false, Duration: time.Millisecond}},
	})
	// circuit-open fail-fast: no HTTP attempt made at all, nil records.
	recordAgentOutcome(Result{
		Status: StatusFailed,
		Err:    &llmclient.CircuitOpenError{Provider: "groq"},
	})

	if got := metrics.Counter(metrics.NameAPICallsTotal).Value(); got != 0 {
		t.Errorf("atcr_api_calls_total = %d, want 0 (no attempt reached the wire)", got)
	}
	if got := metrics.Histogram(metrics.NameAPICallDurationSeconds).Count(); got != 0 {
		t.Errorf("%s count = %d, want 0 (no completed HTTP attempt)", metrics.NameAPICallDurationSeconds, got)
	}
}

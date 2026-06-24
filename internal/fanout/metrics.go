package fanout

import (
	"context"
	"errors"
	reclib "github.com/samestrin/atcr/reconcile"
	"strconv"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/stream"
)

// recordAgentOutcome translates one finished agent Result into metric updates:
// the success/failure/timeout tally, the API-call count, the per-call latency
// histogram, the per-HTTP-status API-error counter (when the error unwraps to
// *llmclient.HTTPStatusError), and the tool-call total.
//
// API calls are counted PER ATTEMPT (Epic 4.11): apiCallCount sums the agent's
// CallRecords that reached the wire — retries included — and each such record's
// duration is observed into atcr_api_call_duration_seconds, so on the per-record
// path the histogram's sample count equals atcr_api_calls_total (the nil Turns
// fallback below increments the counter without emitting a latency sample, so the
// two can diverge only for completers that surface no per-call data). This is a deliberate change
// from the prior Turns-based one-per-round-trip behavior: a single-shot that
// retried (defaultMaxRetries = 2) now counts each HTTP attempt rather than 1, so
// dashboards comparing pre/post values will see a one-time inflation step.
// Results without per-call data fall back to the old Turns heuristic (see
// apiCallCount). errors.As tolerates a nil Result.Err, so non-error results skip
// the API-error counter without a guard.
func recordAgentOutcome(r Result) {
	switch r.Status {
	case StatusOK:
		metrics.Counter(metrics.NameAgentsSucceeded).Inc()
	case StatusFailed:
		metrics.Counter(metrics.NameAgentsFailed).Inc()
	case StatusTimeout:
		metrics.Counter(metrics.NameAgentsTimedOut).Inc()
	}

	metrics.Counter(metrics.NameAPICallsTotal).Add(int64(apiCallCount(r)))

	// One latency observation per HTTP attempt that reached the wire (a completed
	// HTTP attempt, AC3); non-wire records (cancel-before-send) and the nil
	// fallback contribute nothing, keeping the sample count equal to the call
	// count.
	for _, rec := range r.CallRecords {
		if rec.ReachedWire {
			metrics.Histogram(metrics.NameAPICallDurationSeconds).Observe(rec.Duration.Seconds())
		}
	}

	var he *llmclient.HTTPStatusError
	if errors.As(r.Err, &he) && he.Status > 0 {
		metrics.Counter(metrics.Key(metrics.NameAPIErrorsTotal, metrics.LabelStatus, strconv.Itoa(he.Status))).Inc()
	}

	if r.ToolCalls > 0 {
		metrics.Counter(metrics.NameToolCallsTotal).Add(int64(r.ToolCalls))
	}
}

// apiCallCount derives the number of provider HTTP round-trips for one agent
// result. When the client surfaced per-call telemetry (CallRecords non-nil) it
// counts one per attempt that reached the wire — retries included — so the count
// is the true number of HTTP requests and matches the latency histogram's sample
// count (AC1: a single-shot whose context expired after one round-trip has one
// wire record and counts 1; AC2: a cancel-before-send record never reached the
// wire and counts 0).
//
// When no per-call data is present — a Complete-only test double, or a
// circuit-open fail-fast that made no request and surfaces nil records (AC2) — it
// falls back to the prior Turns-based heuristic: Turns round-trips for a tool
// loop, one for a single-shot, and zero when a context error or circuit-open
// means no request was made. A corrupt negative Turns is also treated as a
// single-shot rather than decrementing the monotonic counter.
func apiCallCount(r Result) int {
	if r.CallRecords != nil {
		n := 0
		for _, rec := range r.CallRecords {
			if rec.ReachedWire {
				n++
			}
		}
		return n
	}

	apiCalls := r.Turns
	if apiCalls < 1 {
		var coe *llmclient.CircuitOpenError
		switch {
		case errors.Is(r.Err, context.DeadlineExceeded) || errors.Is(r.Err, context.Canceled):
			apiCalls = 0
		case errors.As(r.Err, &coe):
			apiCalls = 0
		default:
			apiCalls = 1
		}
	}
	return apiCalls
}

// recordFindingMetrics counts findings from one review: the total and a
// per-severity breakdown. The caller (WritePool) passes the post-guardrail set
// produced by enforceConstraints (min_severity floor + max_findings cap), so
// these counts reflect kept findings, not raw agent output. Recorded once in
// WritePool so both the CLI and the MCP server observe the same numbers.
func recordFindingMetrics(findings []stream.Finding) {
	if len(findings) == 0 {
		return
	}
	metrics.Counter(metrics.NameFindingsTotal).Add(int64(len(findings)))
	for _, f := range findings {
		sev := reclib.NormalizeSeverity(f.Severity)
		if _, ok := reclib.SeverityRank[sev]; !ok {
			sev = "UNKNOWN"
		}
		metrics.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, sev)).Inc()
	}
}

// recordReviewOutcome records one review's terminal classification. An interrupt
// (SIGINT/SIGTERM) takes precedence over a failure: a review whose agents were
// all cut off by a signal is interrupted, not failed.
func recordReviewOutcome(interrupted, failed bool) {
	switch {
	case interrupted:
		metrics.Counter(metrics.NameReviewsInterrupted).Inc()
	case failed:
		metrics.Counter(metrics.NameReviewsFailed).Inc()
	default:
		metrics.Counter(metrics.NameReviewsSucceeded).Inc()
	}
}

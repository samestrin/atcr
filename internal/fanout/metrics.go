package fanout

import (
	"context"
	"errors"
	"strconv"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
	"github.com/samestrin/atcr/internal/stream"
)

// recordAgentOutcome translates one finished agent Result into metric updates:
// the success/failure/timeout tally, the API-call count, the per-HTTP-status
// API-error counter (when the error unwraps to *llmclient.HTTPStatusError — the
// public type the clarification settled on, so no internal/llmclient edits are
// needed), and the tool-call total. API calls are counted as provider
// round-trips: a tool-loop agent makes one Chat call per turn (Result.Turns),
// while the single-shot path leaves Turns at 0 and makes exactly one call, so
// max(1, Turns) is the round-trip count. Slot-level fallbacks are separate
// invokeAgent calls and so are counted independently, covering "API calls
// (including retries)" at the granularity the fan-out can observe. API call
// latency is intentionally not recorded: the per-call start time lives inside the
// client and is not surfaced on Result, so the histogram is deferred (Epic 4.4
// clarification; AC7 needs only the error counter). errors.As tolerates a nil
// Result.Err, so non-error results skip the API-error counter without a guard.
func recordAgentOutcome(r Result) {
	switch r.Status {
	case StatusOK:
		metrics.Counter(metrics.NameAgentsSucceeded).Inc()
	case StatusFailed:
		metrics.Counter(metrics.NameAgentsFailed).Inc()
	case StatusTimeout:
		metrics.Counter(metrics.NameAgentsTimedOut).Inc()
	}

	apiCalls := r.Turns
	if apiCalls == 0 {
		// Single-shot path normally makes exactly one provider round-trip, but a
		// context cancellation/deadline that fires before the first HTTP call leaves
		// Turns at 0 with no actual request — don't inflate the counter.
		if !errors.Is(r.Err, context.DeadlineExceeded) && !errors.Is(r.Err, context.Canceled) {
			apiCalls = 1
		}
	}
	metrics.Counter(metrics.NameAPICallsTotal).Add(int64(apiCalls))

	var he *llmclient.HTTPStatusError
	if errors.As(r.Err, &he) {
		metrics.Counter(metrics.Key(metrics.NameAPIErrorsTotal, metrics.LabelStatus, strconv.Itoa(he.Status))).Inc()
	}

	if r.ToolCalls > 0 {
		metrics.Counter(metrics.NameToolCallsTotal).Add(int64(r.ToolCalls))
	}
}

// recordFindingMetrics counts the findings emitted by the agents of one review:
// the total and a per-severity breakdown. These are raw per-agent findings (the
// metric definition is "emitted by agents"), recorded once in WritePool so both
// the CLI and the MCP server observe the same numbers.
func recordFindingMetrics(findings []stream.Finding) {
	if len(findings) == 0 {
		return
	}
	metrics.Counter(metrics.NameFindingsTotal).Add(int64(len(findings)))
	for _, f := range findings {
		sev := stream.NormalizeSeverity(f.Severity)
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

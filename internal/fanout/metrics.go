package fanout

import (
	"errors"
	"strconv"

	"github.com/samestrin/atcr/internal/llmclient"
	"github.com/samestrin/atcr/internal/metrics"
)

// Metric names recorded by the fan-out engine (Epic 4.4). Kept as constants so a
// rename is a single edit; tests assert the literal strings so a typo here is
// caught rather than masked.
const (
	metricAgentsTotal          = "atcr_agents_total"
	metricAgentsSucceeded      = "atcr_agents_succeeded"
	metricAgentsFailed         = "atcr_agents_failed"
	metricAgentsTimedOut       = "atcr_agents_timed_out"
	metricAgentDurationSeconds = "atcr_agent_duration_seconds"
	metricAPICallsTotal        = "atcr_api_calls_total"
	metricAPIErrorsTotal       = "atcr_api_errors_total"
	metricToolCallsTotal       = "atcr_tool_calls_total"
)

// recordAgentOutcome translates one finished agent Result into metric updates:
// the success/failure/timeout tally, the per-HTTP-status API-error counter
// (when the error unwraps to *llmclient.HTTPStatusError — the public type the
// clarification settled on, so no internal/llmclient edits are needed), and the
// tool-call total. API call latency is intentionally not recorded here: the
// per-call start time lives inside the client and is not surfaced on Result, so
// the histogram is deferred (Epic 4.4 clarification; AC7 needs only the error
// counter). errors.As tolerates a nil Result.Err, so non-error results skip the
// API-error counter without a guard.
func recordAgentOutcome(r Result) {
	switch r.Status {
	case StatusOK:
		metrics.Counter(metricAgentsSucceeded).Inc()
	case StatusFailed:
		metrics.Counter(metricAgentsFailed).Inc()
	case StatusTimeout:
		metrics.Counter(metricAgentsTimedOut).Inc()
	}

	var he *llmclient.HTTPStatusError
	if errors.As(r.Err, &he) {
		metrics.Counter(metrics.Key(metricAPIErrorsTotal, "status", strconv.Itoa(he.Status))).Inc()
	}

	if r.ToolCalls > 0 {
		metrics.Counter(metricToolCallsTotal).Add(int64(r.ToolCalls))
	}
}

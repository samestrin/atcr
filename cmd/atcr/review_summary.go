package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/samestrin/atcr/internal/metrics"
)

// severityOrder is the high-to-low display order for the findings breakdown.
var severityOrder = []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}

// summarySnapshot captures the values of the counters writeReviewSummary reports,
// read from a registry at a point in time. Diffing a post-review snapshot against a
// pre-review baseline (snapshotSummaryMetrics before fan-out, sub after) isolates a
// single review's contribution. That keeps the summary correct even in a long-lived
// process where DefaultRegistry accumulates across reviews (serve mode) instead of
// holding one review's totals — so the line can never report succeeded greater than
// totalAgents. Sourcing timed-out from atcr_agents_timed_out here preserves the
// "timed out" breakdown without adding a TimedOut field to fanout.Summary.
type summarySnapshot struct {
	agentsSucceeded    int64
	agentsFailed       int64
	agentsTimedOut     int64
	apiCalls           int64
	findingsTotal      int64
	findingsBySeverity map[string]int64
}

// snapshotSummaryMetrics reads the current values of the metrics the review summary
// reports from reg. Take one before fan-out and one after, then sub to get the
// review's deltas.
func snapshotSummaryMetrics(reg *metrics.Registry) summarySnapshot {
	bySeverity := make(map[string]int64, len(severityOrder))
	for _, sev := range severityOrder {
		bySeverity[sev] = reg.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, sev)).Value()
	}
	return summarySnapshot{
		agentsSucceeded:    reg.Counter(metrics.NameAgentsSucceeded).Value(),
		agentsFailed:       reg.Counter(metrics.NameAgentsFailed).Value(),
		agentsTimedOut:     reg.Counter(metrics.NameAgentsTimedOut).Value(),
		apiCalls:           reg.Counter(metrics.NameAPICallsTotal).Value(),
		findingsTotal:      reg.Counter(metrics.NameFindingsTotal).Value(),
		findingsBySeverity: bySeverity,
	}
}

// sub returns s minus baseline per metric — this review's contribution to each
// counter when s is the post-review snapshot and baseline the pre-review one.
func (s summarySnapshot) sub(baseline summarySnapshot) summarySnapshot {
	out := summarySnapshot{
		agentsSucceeded:    s.agentsSucceeded - baseline.agentsSucceeded,
		agentsFailed:       s.agentsFailed - baseline.agentsFailed,
		agentsTimedOut:     s.agentsTimedOut - baseline.agentsTimedOut,
		apiCalls:           s.apiCalls - baseline.apiCalls,
		findingsTotal:      s.findingsTotal - baseline.findingsTotal,
		findingsBySeverity: make(map[string]int64, len(severityOrder)),
	}
	for _, sev := range severityOrder {
		out.findingsBySeverity[sev] = s.findingsBySeverity[sev] - baseline.findingsBySeverity[sev]
	}
	return out
}

// writeReviewSummary prints the end-of-review metrics summary (Epic 4.4 AC3):
// duration, agent success/failure/timeout counts, API calls, and findings with a
// severity breakdown. m holds this review's deltas (post-review snapshot minus the
// pre-review baseline), so the counts reflect this review alone rather than the
// process-cumulative registry — these are the same metrics the MCP server exports.
// m is a parameter so the helper is unit testable against a seeded snapshot.
func writeReviewSummary(w io.Writer, m summarySnapshot, elapsed time.Duration, totalAgents int) {
	// elapsed is total wall-clock from before config load to review completion,
	// not just the agent fan-out window the atcr_review_duration_seconds histogram measures.
	_, _ = fmt.Fprintf(w, "Total elapsed: %.1fs\n", elapsed.Seconds())
	_, _ = fmt.Fprintf(w, "Agents: %d/%d succeeded, %d failed, %d timed out\n",
		m.agentsSucceeded, totalAgents, m.agentsFailed, m.agentsTimedOut)
	_, _ = fmt.Fprintf(w, "API calls: %d\n", m.apiCalls)
	_, _ = fmt.Fprintf(w, "Findings: %d%s\n", m.findingsTotal, severityBreakdown(m))
}

// severityBreakdown renders " (2 HIGH, 3 MEDIUM)" for the non-zero severities in
// high-to-low order, or "" when no findings were recorded.
func severityBreakdown(m summarySnapshot) string {
	var parts []string
	for _, sev := range severityOrder {
		if n := m.findingsBySeverity[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

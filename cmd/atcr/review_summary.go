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

// writeReviewSummary prints the end-of-review metrics summary (Epic 4.4 AC3):
// duration, agent success/failure/timeout counts, API calls, and findings with a
// severity breakdown. It reads the counts from reg — in production the
// process-global DefaultRegistry, which (a CLI process runs exactly one review)
// holds this review's totals — so the summary is a direct readout of the same
// metrics the MCP server exports. reg is a parameter so the helper is unit
// testable against a seeded registry.
func writeReviewSummary(w io.Writer, reg *metrics.Registry, elapsed time.Duration, totalAgents int) {
	val := func(name string) int64 { return reg.Counter(name).Value() }

	// elapsed is total wall-clock from before config load to review completion,
	// not just the agent fan-out window the atcr_review_duration_seconds histogram measures.
	_, _ = fmt.Fprintf(w, "Total elapsed: %.1fs\n", elapsed.Seconds())
	_, _ = fmt.Fprintf(w, "Agents: %d/%d succeeded, %d failed, %d timed out\n",
		val(metrics.NameAgentsSucceeded), totalAgents,
		val(metrics.NameAgentsFailed), val(metrics.NameAgentsTimedOut))
	_, _ = fmt.Fprintf(w, "API calls: %d\n", val(metrics.NameAPICallsTotal))
	_, _ = fmt.Fprintf(w, "Findings: %d%s\n", val(metrics.NameFindingsTotal), severityBreakdown(reg))
}

// severityBreakdown renders " (2 HIGH, 3 MEDIUM)" for the non-zero severities in
// high-to-low order, or "" when no findings were recorded.
func severityBreakdown(reg *metrics.Registry) string {
	var parts []string
	for _, sev := range severityOrder {
		if n := reg.Counter(metrics.Key(metrics.NameFindingsBySeverity, metrics.LabelSeverity, sev)).Value(); n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, sev))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
